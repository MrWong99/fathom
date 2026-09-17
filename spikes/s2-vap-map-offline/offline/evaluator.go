// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package offline

import (
	"context"
	"fmt"
	"io/fs"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/policy/generic"
	"k8s.io/apiserver/pkg/admission/plugin/policy/matching"
	"k8s.io/apiserver/pkg/admission/plugin/policy/mutating"
	"k8s.io/apiserver/pkg/admission/plugin/policy/validating"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/cel/environment"
	"k8s.io/apiserver/pkg/warning"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/kubectl-validate/pkg/openapiclient"

	"github.com/MrWong99/fathom/spikes/s2-vap-map-offline/port"
)

// Options configure the evaluator.
type Options struct {
	// CompatibilityVersion is the version handed to environment.MustBaseEnvSet.
	// The apiserver uses environment.DefaultCompatibilityVersion(), which is
	// the binary's MinCompatibilityVersion (binary minor - 1: 1.36 for the
	// 1.37 oracle). The offline caller pins it explicitly instead of reading
	// the process-global component registry. Nil means 1.36.
	CompatibilityVersion *version.Version
	// Authorizer answers the `authorizer` CEL variable. Nil means
	// NewSnapshotAuthorizer().
	Authorizer authorizer.UnconditionalAuthorizer
}

// Evaluator holds compiled policies and the snapshot-backed dependencies the
// apiserver's VAP and MAP plugins need.
type Evaluator struct {
	objectInterfaces admission.ObjectInterfaces
	user             user.Info
	authz            authorizer.UnconditionalAuthorizer

	vapDispatcher generic.Dispatcher[validating.PolicyHook]
	vapHooks      []validating.PolicyHook
	mapDispatcher generic.Dispatcher[mutating.PolicyHook]
	mapHooks      []mutating.PolicyHook

	stopOnce sync.Once
	stop     chan struct{}
}

// Result is what the apiserver would have answered for one CREATE.
type Result struct {
	// Object is the request object after MutatingAdmissionPolicies ran.
	Object runtime.Object
	// Err is nil when admitted; otherwise the *StatusError the apiserver
	// would return (VAP denial or MAP failure).
	Err error
	// Warnings are the warning headers in emission order.
	Warnings []string
	// AuditAnnotations are the admission audit annotations recorded on the
	// attributes (VAP Audit action and auditAnnotations).
	AuditAnnotations map[string]string
}

// Allowed reports whether the request would have been admitted.
func (r Result) Allowed() bool { return r.Err == nil }

var (
	deploymentGVK = appsv1.SchemeGroupVersion.WithKind("Deployment")
	deploymentGVR = appsv1.SchemeGroupVersion.WithResource("deployments")
	configMapGVR  = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}
)

// New compiles every policy in the snapshot the way the apiserver's plugins
// do and wires the snapshot-backed matcher, param informer, authorizer and
// type converter.
func New(snap *Snapshot, openapiFS fs.FS, opts Options) (*Evaluator, error) {
	ver := opts.CompatibilityVersion
	if ver == nil {
		ver = version.MajorMinor(1, 36)
	}
	envSet := environment.MustBaseEnvSet(ver)

	authz := opts.Authorizer
	if authz == nil {
		authz = NewSnapshotAuthorizer()
	}

	// Namespace lister over the snapshot namespace; the fake clientset is the
	// NotFound fallback the namespace matcher consults and the source for the
	// param informer.
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := indexer.Add(snap.Namespace); err != nil {
		return nil, err
	}
	nsLister := listersv1.NewNamespaceLister(indexer)

	var objs []runtime.Object
	objs = append(objs, snap.Namespace)
	for _, cm := range snap.ConfigMaps {
		objs = append(objs, cm)
	}
	client := fake.NewClientset(objs...)
	matcher := matching.NewMatcher(nsLister, client)

	e := &Evaluator{
		objectInterfaces: admission.NewObjectInterfacesFromScheme(clientgoscheme.Scheme),
		user:             snap.User,
		authz:            authz,
		stop:             make(chan struct{}),
	}

	// Param informer: the apiserver's policy source creates one typed
	// informer per paramKind through the shared informer factory; the fake
	// clientset serves the snapshot ConfigMaps to it.
	factory := informers.NewSharedInformerFactory(client, 0)
	paramInformer, err := factory.ForResource(configMapGVR)
	if err != nil {
		return nil, err
	}
	factory.Start(e.stop)
	for gvr, synced := range factory.WaitForCacheSync(e.stop) {
		if !synced {
			return nil, fmt.Errorf("informer for %v did not sync", gvr)
		}
	}

	for _, p := range snap.VAPs {
		hook := validating.PolicyHook{Policy: p, Evaluator: port.CompileValidatingPolicy(p, envSet)}
		for _, b := range snap.VAPBindings {
			if b.Spec.PolicyName == p.Name {
				hook.Bindings = append(hook.Bindings, b)
			}
		}
		if pk := p.Spec.ParamKind; pk != nil {
			if pk.APIVersion == "v1" && pk.Kind == "ConfigMap" {
				hook.ParamInformer = paramInformer
				hook.ParamScope = meta.RESTScopeNamespace
			} else {
				hook.ConfigurationError = fmt.Errorf("paramKind %s/%s is not in the snapshot", pk.APIVersion, pk.Kind)
			}
		}
		e.vapHooks = append(e.vapHooks, hook)
	}
	e.vapDispatcher = port.NewValidatingDispatcher(authz, generic.NewPolicyMatcher(matcher))

	tcm, err := NewSnapshotTypeConverterManager(openapiclient.NewLocalSchemaFiles(openapiFS))
	if err != nil {
		return nil, err
	}
	for _, p := range snap.MAPs {
		hook := mutating.PolicyHook{Policy: p, Evaluator: port.CompileMutatingPolicy(p, envSet)}
		if hook.Evaluator.Error != nil {
			hook.ConfigurationError = hook.Evaluator.Error
		}
		for _, b := range snap.MAPBindings {
			if b.Spec.PolicyName == p.Name {
				hook.Bindings = append(hook.Bindings, b)
			}
		}
		if pk := p.Spec.ParamKind; pk != nil {
			if pk.APIVersion == "v1" && pk.Kind == "ConfigMap" {
				hook.ParamInformer = paramInformer
				hook.ParamScope = meta.RESTScopeNamespace
			} else {
				hook.ConfigurationError = fmt.Errorf("paramKind %s/%s is not in the snapshot", pk.APIVersion, pk.Kind)
			}
		}
		e.mapHooks = append(e.mapHooks, hook)
	}
	e.mapDispatcher = port.NewMutatingDispatcher(authz, matcher, tcm)
	return e, nil
}

// Close stops the param informer.
func (e *Evaluator) Close() { e.stopOnce.Do(func() { close(e.stop) }) }

// Admit runs the apiserver's admission order for a dry-run CREATE of a
// Deployment: MutatingAdmissionPolicy (with the two-pass reinvocation loop),
// then ValidatingAdmissionPolicy on the mutated object. The input is not
// modified.
func (e *Evaluator) Admit(ctx context.Context, d *appsv1.Deployment) Result {
	obj := d.DeepCopy()
	attrs := &annotatedAttributes{
		Attributes:  admission.NewAttributesRecord(obj, nil, deploymentGVK, obj.Namespace, obj.Name, deploymentGVR, "", admission.Create, &metav1.CreateOptions{}, true, e.user),
		annotations: map[string]string{},
	}
	rec := &warningRecorder{}
	ctx = warning.WithWarningRecorder(ctx, rec)

	res := Result{Object: obj}
	err := port.AdmitWithReinvocation(ctx, attrs, e.objectInterfaces, func(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
		return e.mapDispatcher.Dispatch(ctx, a, o, e.mapHooks)
	})
	// The mutating dispatcher writes the versioned object back with the
	// scheme's self-conversion, which clears TypeMeta the way the apiserver's
	// external-to-internal conversion does. The apiserver's admission object
	// is the internal type and the response encoder sets apiVersion/kind
	// again; offline the object stays external, so restore the kind here.
	obj.GetObjectKind().SetGroupVersionKind(deploymentGVK)
	if err == nil {
		err = e.vapDispatcher.Dispatch(ctx, attrs, e.objectInterfaces, e.vapHooks)
	}
	res.Err = err
	res.Warnings = rec.warnings
	res.AuditAnnotations = attrs.annotations
	return res
}

// annotatedAttributes captures the admission audit annotations the plugins
// add (the apiserver's audit handler wraps attributes the same way).
type annotatedAttributes struct {
	admission.Attributes
	annotations map[string]string
}

func (a *annotatedAttributes) AddAnnotation(key, value string) error {
	return a.AddAnnotationWithLevel(key, value, auditinternal.LevelMetadata)
}

func (a *annotatedAttributes) AddAnnotationWithLevel(key, value string, level auditinternal.Level) error {
	if err := a.Attributes.AddAnnotationWithLevel(key, value, level); err != nil {
		return err
	}
	a.annotations[key] = value
	return nil
}

type warningRecorder struct {
	warnings []string
}

func (w *warningRecorder) AddWarning(_, text string) { w.warnings = append(w.warnings, text) }
