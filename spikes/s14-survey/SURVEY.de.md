# fathom Praktiker-Umfrage (S14)

*Entwurf 1, 2026-09-17. Alles von „Teil 1“ bis zum Ende von „Teil 4“ ist der
Fragebogen, der verschickt wird. „Teil 5“ ist das Protokoll für die
Hands-on-Session mit drei Consultants. Der „Anhang für den Absender“ am Ende ist
nur für den Owner: Er enthält die Entscheidungsregeln, die festgelegt werden,
bevor die erste Antwort eintrifft. Vor dem Versand löschen.*

---

## Einleitungstext (unverändert versenden)

Wir entscheiden gerade, was ein neuer Pre-Merge-Check für Kubernetes- und
Compose-Deployments zuerst abfangen soll. Das Tool wird eine Helm-,
Kustomize- oder Compose-Änderung rendern und gegen einen Read-only-Snapshot
des Zielclusters validieren, bevor der Pull Request geöffnet wird, sodass
Quota-, Policy-, Schema- und Referenzfehler auf Deinem Laptop abgefangen
werden statt erst beim Sync.

Diese Umfrage dauert 10 bis 15 Minuten. Sie fragt, was bei Dir heute tatsächlich
kaputtgeht, welche Cluster und Repositories Deine Kunden betreiben und wie Du
Values bearbeitest. Die Antworten sind anonym; die Kundenzeilen in Teil 3 fragen
nur nach Kategorien, nicht nach Namen. Die Freitextantworten am Ende sind der
wertvollste Teil.

Die Fragen-IDs (A1, B1.3, C2.1, ...) dienen der Auswertung; ignoriere sie.

---

## Teil 1: Über Dich (3 Fragen)

**A1. Welche Rolle beschreibt den Großteil Deiner Arbeit?** (eine Antwort)
- Entwickler: Ich schreibe die Charts, Compose-Dateien oder Anwendungsmanifeste
- Consultant: Ich deploye unsere Software in Kundencluster
- Supporter: Ich betreibe Installationen, die bereits deployt sind, oder behebe Störungen darin
- Plattform: Ich betreibe Cluster, Policies und GitOps-Controller
- Gemischt: zwei oder mehr der obigen Rollen zu etwa gleichen Teilen

**A2. Wie lange arbeitest Du schon mit Kubernetes?** (eine Antwort)
- Weniger als 1 Jahr
- 1 bis 3 Jahre
- 3 bis 6 Jahre
- Mehr als 6 Jahre

**A3. In wie vielen verschiedenen Kundenclustern hast Du in den letzten 12 Monaten deployt oder supportet?** (eine Antwort)
- 0
- 1 bis 2
- 3 bis 5
- 6 bis 10
- Mehr als 10

---

## Teil 2: Was kaputtgeht (2 Matrixfragen und 3 Fragen)

Denk an die Deployment-Änderungen der letzten drei Monate bei Dir und in Deinem
Team: Values-Änderungen, Chart-Upgrades, neue Umgebungen, Compose-Änderungen.

**B1. Wie oft hat Dich jede der folgenden Fehlerarten in den letzten drei
Monaten getroffen?** (Matrixfrage; eine Antwort pro Zeile)

Spalten: Nie | Ein- oder zweimal | Etwa monatlich | Etwa wöchentlich | Täglich oder öfter

| ID | Fehlerart (mit der Meldung, die Du wiedererkennen würdest) |
|---|---|
| B1.1 | YAML-Syntax- oder Typ-Überraschung: Einrückung, `NO`/`ON` als Boolean gelesen, `1e3` als Zahl gelesen, ein Schlüssel stillschweigend ignoriert |
| B1.2 | Falscher Wertetyp oder falsche Einheit in den Values: `1000M` statt `1000m`, ein String, wo eine Zahl erwartet wurde, ein falsch geschriebener Schlüssel, den niemand bemerkt hat |
| B1.3 | Gerendertes Manifest vom API-Schema abgelehnt: `unknown field`, falscher Typ, falsche `apiVersion` |
| B1.4 | API-Version auf der Zielclusterversion entfernt oder deprecated (`no matches for kind ... in version ...`) |
| B1.5 | Eine Custom Resource von ihrer CRD-Validierungsregel abgelehnt (Gateway API, Crossplane, Flux, Operator-CRs mit feldübergreifenden Regeln) |
| B1.6 | Admission-Policy hat abgelehnt: Kyverno, Gatekeeper/OPA, ValidatingAdmissionPolicy, Kubewarden (Registry nicht erlaubt, Label fehlt, privilegierter Container) |
| B1.7 | Pod Security Admission: Das Deployment wurde akzeptiert, aber kein Pod erschien (`violates PodSecurity "restricted:latest"`) |
| B1.8 | ResourceQuota überschritten (`exceeded quota: ... requested: 2000m, available: 1000m`) |
| B1.9 | LimitRange hat den Pod abgelehnt (min/max/ratio) oder Defaults gesetzt, die Du nicht erwartet hast |
| B1.10 | Ein referenziertes Objekt fehlte: Secret- oder ConfigMap-Schlüssel, StorageClass, IngressClass, PriorityClass, ServiceAccount (`CreateContainerConfigError`, PVC hängt in Pending) |
| B1.11 | Image nicht pullbar: Tag existiert nicht, Registry blockiert oder nicht gespiegelt, Image nicht signiert (`ImagePullBackOff`) |
| B1.12 | Dem GitOps-Controller fehlten Berechtigungen (`forbidden: User system:serviceaccount:... cannot create resource`) |
| B1.13 | Immutable Field oder Ownership-Konflikt: Selector, `clusterIP`, PVC-Storage-Class, Field-Manager-Konflikte zwischen Helm, Argo CD, Flux oder einer mutierenden Policy |
| B1.14 | Ein Webhook, den Du nicht kontrollierst, hat das Objekt mit einer wenig hilfreichen Meldung abgelehnt oder verändert (cert-manager, Hersteller-Mutatoren, Cloud-Provider-Webhooks) |
| B1.15 | Nicht schedulbar: Taints, Node-Selectors, Node-Pools, Autopilot-Ressourcenverhältnisse; Pods hängen in Pending |
| B1.16 | Der effektive Wert nach dem Zusammenführen der Layer war nicht das, was Du erwartet hast (Basis, Umgebung, Cluster-Overrides), oder es lief in Dev und nicht in Prod |
| B1.17 | Docker Compose: Eine nicht gesetzte Variable wurde leer, ein Port war bereits belegt, ein Bind-Pfad fehlte, `deploy:` wurde ignoriert, `container_name` kollidierte |
| B1.18 | OpenShift-spezifisch: SCC- oder UID-Bereichs-Ablehnung (`runAsUser ... out of range`, `restricted-v2` lehnt ab), Route-Host-Konflikt, Projekt-Template-Quota, von der Du nichts wusstest |

**B2. Für dieselben Arten: Wo wurde der Fehler üblicherweise entdeckt?** (Matrixfrage;
eine Antwort pro Zeile; lass eine Zeile leer, wenn es nie vorkam)

Spalten: Vor dem Push, auf meinem Rechner | In der CI, vor dem Merge | Beim GitOps-Sync oder `helm upgrade`, nach dem Merge | Pods hängen in Pending oder crashen, oder erst zur Laufzeit | Der Kunde hat es zuerst bemerkt

Zeilen: B2.1 bis B2.18, dieselben Arten wie in B1.

**B3. Welche drei Arten haben Dich in den letzten drei Monaten die meisten Arbeitsstunden gekostet?**
(wähle bis zu drei nach Nummer, die teuerste zuerst)

B3.first (Platz 1): ____ B3.second (Platz 2): ____ B3.third (Platz 3): ____

**B4. Für die teuerste davon: Wie viele Arbeitsstunden hat sie in den letzten
drei Monaten ungefähr gekostet, über Dein ganzes Team hinweg?** (eine Antwort)
- Unter 2 Stunden
- 2 bis 8 Stunden
- 1 bis 3 Arbeitstage
- Mehr als 3 Arbeitstage

**B5. Fehlt eine Fehlerart in der Liste?** (Freitext, optional)

---

## Teil 3: Die Cluster, in die Du deployst (pro Kunde, bis zu fünf)

Fülle eine Spalte pro Kundencluster aus, in den Du in den letzten 12 Monaten
deployt oder den Du supportet hast, den aktuellsten zuerst. Wenn es mehr als fünf
sind, nimm die fünf, mit denen Du die meiste Zeit verbracht hast. Nur
Kategorien; keine Kundennamen. Entwickler, die nicht deployen: weiter zu Teil 4.

| ID | Frage | Kunde 1 | Kunde 2 | Kunde 3 | Kunde 4 | Kunde 5 |
|---|---|---|---|---|---|---|
| C1 | Distribution: OpenShift / AKS / EKS / GKE Standard / GKE Autopilot / Rancher RKE2 oder K3s / Tanzu / kubeadm oder anderes Vanilla / andere | | | | | |
| C2 | Kubernetes-Minor-Version der Produktion: 1.30 oder älter / 1.31 / 1.32 / 1.33 / 1.34 / 1.35 / 1.36 / 1.37 / weiß nicht | | | | | |
| C3 | Wie Helm dort läuft: Argo CD / Flux / Helmfile / CI-Pipeline führt `helm upgrade` aus / eine Person führt `helm upgrade` vom Laptop aus / Rancher Fleet / OLM-Operator / Kustomize `helmCharts` / kein Helm, reine Manifeste oder Kustomize | | | | | |
| C4 | Wo das Konfigurations-Repository liegt: GitHub.com / GitHub Enterprise / GitLab.com / GitLab self-managed / Azure DevOps / Bitbucket / Gitea oder Forgejo / andere / es gibt kein Git-Repository, Values werden per Mail oder Ticket übergeben | | | | | |
| C5 | Aktive Policy-Engines (Enforce-Modus, alle nennen): Kyverno / Gatekeeper / ValidatingAdmissionPolicy oder MutatingAdmissionPolicy / Kubewarden / Azure Policy / keine / weiß nicht | | | | | |
| C6 | Deine eigenen Berechtigungen auf diesem Cluster: cluster-admin / clusterweit lesend plus Namespace-Admin / nur auf Namespaces beschränkt / kein direkter Zugriff, der Kunde spielt die Änderungen ein / weiß nicht | | | | | |
| C7 | Netzwerk: air-gapped / Egress eingeschränkt mit Mirror-Registry / offen | | | | | |
| C8 | Secrets: External Secrets Operator / Sealed Secrets / SOPS / Vault-Injector / manuell angelegt / weiß nicht | | | | | |
| C9 | Anzahl der Umgebungen für unsere Software bei diesem Kunden (Dev, Staging, Prod, ...): 1 / 2 / 3 / 4 oder mehr | | | | | |
| C10 | Repository-Layout: DRY, der Controller rendert Values und Overlays / hydrated, gerenderte Manifeste werden committet / beides / weiß nicht | | | | | |
| C11 | Regulierter Kontext (BSI, ISO 27001, KRITIS, FIPS oder ähnliche Audit-Anforderungen gelten für das Deployment): ja / nein / weiß nicht | | | | | |
| C12 | Docker-Compose-Deployments unserer Software bei diesem Kunden: ja, in Produktion / ja, nur Dev oder Test / nein | | | | | |
| C13 | Wer stellt den Pull/Merge Request für die Änderung am Konfigurations-Repository: ich / der Kunde / ich pushe direkt ohne Review / es gibt kein Repository | | | | | |

---

## Teil 4: Wie Du heute arbeitest (9 Fragen)

**D1. Bevor Du einen Pull Request oder Merge Request mit einer Values-Änderung
öffnest: Was führst Du auf Deinem Rechner aus?** (alles Zutreffende)
- `helm template` oder `helm lint`
- kubeconform oder kubeval
- `kubectl apply --dry-run=server` gegen den Zielcluster
- `kyverno apply`, `gator test` oder `conftest`
- `argocd app diff` oder `flux diff`
- Ein Skript oder Makefile, das das Team pflegt
- Nichts; die CI oder der Controller sagt es mir
- Sonstiges: ____

**D2. Wo bearbeitest Du Umgebungs-Values?** (alles Zutreffende)
- Editor mit angehängtem YAML-Schema (VS Code YAML-Extension, IntelliJ): welcher: ____
- Editor ohne Schema
- Argo CD UI, Parameter-Tab
- Ein Webformular (Rancher, Kubeapps, eine Herstellerkonsole)
- Direkt im Web-Editor des Git-Hostings
- Sonstiges: ____

**D3. Liefern die Charts, die Du deployst, eine `values.schema.json` mit?** (eine Antwort)
- Immer
- Manchmal
- Selten
- Nie
- Weiß nicht

**D4. Falls Du Charts schreibst: Liefert Dein Chart eine `values.schema.json` mit?** (eine Antwort)
- Ja, von Hand gepflegt
- Ja, aus Kommentaren oder mit einem Tool generiert
- Teilweise
- Nein, aber ich würde es tun, wenn sie aus den bestehenden Values generiert würde
- Nein
- Ich schreibe keine Charts

**D5. Wie lange nach dem Push einer Änderung erfährst Du typischerweise, dass
sie beim Kunden fehlgeschlagen ist?** (eine Antwort)
- Unter 1 Minute
- 1 bis 10 Minuten
- 10 bis 60 Minuten
- Mehrere Stunden
- Am nächsten Tag oder später

**D6. Wäre ein Read-only-Snapshot des Kundenclusters (CRDs, Policies,
Quotas, Namespace-Labels, Secret-Namen, aber nie Secret-Werte) akzeptabel ...**
(eine Antwort pro Zeile)

Spalten: Ja | Nur nach einem Security-Review | Nein | Weiß nicht

- D6.laptop: ... gespeichert auf Deinem Laptop
- D6.repo: ... committet im Konfigurations-Repository
- D6.registry: ... als signiertes Artefakt in eine Container-Registry gepusht

**D7. Was ist die eine Sache, die aus Deiner Sicht unbedingt abgefangen werden sollte, bevor der Pull Request geöffnet wird?**
(Freitext, ein oder zwei Sätze)

**D8. Welche eine Sache würde so ein Tool Deiner Befürchtung nach falsch machen oder verschlimmern?**
(Freitext, ein oder zwei Sätze)

**D9. Dürfen wir Dich für eine 45-minütige Hands-on-Session kontaktieren, in der
Du zwei Values-Änderungen mit einem Editor und mit einem Prototyp-Formular machst?** (eine Antwort)
- Ja: Name oder Handle: ____
- Nein

Danke. Die Ergebnisse gehen an alle zurück, die geantwortet haben.

---

## Teil 5: Hands-on-Session, Formular gegen Editor (Protokoll)

Das ist der aufgabenbasierte Arm aus Design-Abschnitt 3.6. Es ist kein
Fragebogen, sondern eine moderierte 45-minütige Session mit drei Consultants
(rekrutiert über D9), durchgeführt in den Wochen 5 bis 6 des Sprints, sobald der
S9-Formular-Prototyp existiert.

### Aufbau

- Chart: Bitnami redis 28.2.1 mit der synthetischen Layered-Values-Fixture aus
  S6, ersetzt durch das Chart der Firma und echte Layer-Dateien, sobald diese
  verfügbar sind (dann das Ergebnis mit „Firmen-Inputs“ markieren).
- Cluster-Fixture: der kind-Cluster aus den Kalibrierungs-Spikes, mit einem
  `restricted`-Namespace, einer ResourceQuota, einer LimitRange und zwei
  StorageClasses, eine davon RWX-fähig.
- Editor-Arm: VS Code mit der YAML-Extension und dem per Modeline angehängten
  Schema je Umgebung (die `.fathom/schema.<env>.json`-Familie aus
  Design-Abschnitt 3.8) sowie einem `make validate`-Target, das die Änderung
  rendert und `kubectl apply --dry-run=server` gegen den kind-Cluster ausführt.
  Während des Sprints steht das stellvertretend für `fathom validate`; der
  Moderator notiert jede Stelle, an der sich der Stellvertreter unterscheidet.
- Formular-Arm: das S9-Prototyp-Formular, lokal gegen dieselbe Fixture
  bereitgestellt, einschließlich der Anzeige „kommt aktuell aus / wird
  geschrieben nach“.
- Beide Arme enden damit, dass der Teilnehmer einen Pull Request in einem
  Wegwerf-Repository öffnet.

### Aufgaben

Zwei Aufgaben gleicher Größe; jeder Teilnehmer macht eine Aufgabe pro Arm,
sodass niemand eine Aufgabe wiederholt.

- Aufgabe A: für die Umgebung `eu-prod` die Persistenz auf die RWX-fähige
  Storage Class umstellen, das Volume auf 20 Gi vergrößern und jede andere
  Umgebung unverändert lassen. Die Fixture enthält eine Falle: Der Basis-Layer
  setzt die Storage Class per Anker, und der Umgebungs-Layer erbt sie per Alias.
- Aufgabe B: für die Umgebung `eu-prod` den Metrics-Exporter mit einem
  ServiceMonitor aktivieren, CPU- und Memory-Requests setzen, die in die
  Namespace-Quota passen, und die Image-Registry auf den Mirror des Kunden
  zeigen lassen. Die Fixture enthält eine Falle: Die Quota lässt Platz für ein
  ReplicaSet bei den Default-Requests, aber nicht für zwei.

Ausbalancierte Reihenfolge (vor der Rekrutierung festgelegt):

| Teilnehmer | Erster Arm | Zweiter Arm |
|---|---|---|
| P1 | Editor, Aufgabe A | Formular, Aufgabe B |
| P2 | Formular, Aufgabe A | Editor, Aufgabe B |
| P3 | Editor, Aufgabe B | Formular, Aufgabe A |

### Messgrößen (erfasst pro Arm, pro Teilnehmer)

| ID | Messgröße | Wie |
|---|---|---|
| E1 | Zeit bis zu einem Pull Request, der die Validierung besteht | Stoppuhr von der Übergabe der Aufgabenkarte bis zum geöffneten PR |
| E2 | Validierungsfehler vor dem ersten grünen Lauf | Zählung aus dem Tool-Log |
| E3 | Defekte im geöffneten PR | Der Moderator vergleicht den PR mit der Musterlösung: falsche Datei, falsche Umgebung angefasst, Falle nicht behandelt, Wert im falschen Layer |
| E4 | Sicherheit (1 bis 7) | Direkt nach dem Arm gefragt: „Wie sicher bist Du, dass dieser PR korrekt ist?“ |
| E5 | Präferenz | Nach beiden Armen gefragt: Editor / Formular / kommt drauf an, mit einem Satz Begründung |
| E6 | Think-aloud-Notizen | Der Moderator notiert jedes Zögern, jeden Irrweg und jede Nachfrage nach Informationen, die das Tool nicht gezeigt hat |

### Erfassungsbogen

```
Teilnehmer: P_   Datum: ____   Moderator: ____
Arm 1: ______ Aufgabe: _   E1: __:__   E2: __   E3 Defekte: ______________   E4: _
Arm 2: ______ Aufgabe: _   E1: __:__   E2: __   E3 Defekte: ______________   E4: _
E5 Präferenz: ______   warum: _______________________________________________
E6 Notizen: ____________________________________________________________________
```

---

## Anhang für den Absender (vor dem Versand löschen)

### Was jeder Teil entscheidet

| Umfrageteil | Entscheidung, in die er einfließt | Design-Referenz |
|---|---|---|
| B1, B2, B3 | Reihenfolge des Check-Backlogs innerhalb von M1 und in den Monaten 5 bis 6 | Design 10.2; Landscape-Report 3.1 |
| C1, C2 | OpenShift-first-Profil; welche Managed-Distribution das erste Phase-2-Profil bekommt; ob der 1.34/1.35 MAP v1beta1-Fallback relevant ist | Design 0 Punkt 10, Abschnitt 7, S8 |
| C3 | Priorität des `plain`-Layout-Readers gegenüber den Argo/Flux/Helmfile-Readern in M1 | Design 5.1, 12 Punkt 4 |
| C4, C13 | GitLab-Commit-Status- und MR-Note-Adapter in den Monaten 5 bis 6 oder später | Design 3.8, 12 Punkt 3 |
| C5 | Bestätigung von Kyverno-first; ob Gatekeeper Rego (S12) und Kubewarden vorrücken | Design 3.3, 10.3 |
| C6, D6 | Der auf Namespaces beschränkte Erste-Stunde-Pfad; Stufen des Snapshot-Sharings | Design 1.3, 3.5 |
| C7, C11 | Bundle-Export in M1 bleibt; Timing des Regulated-Packs | Design 7, 12 Punkt 6 |
| C8 | Nur ESO-Referenzen oder auch SOPS-Bearbeitung | Design 12 Punkt 7 |
| C10 | Vorrang von hydrated oder DRY | Design 12 Punkt 1 |
| C12 | Timing des Compose-Host-Snapshots | Design 6, 12 Punkt 9 |
| D1, D5 | Der Text „die CI von unten schlagen“ und die zwei Kadenzen | Design 3.6 |
| D2, D3, D4 | Ob der Contract-Emit- und Modeline-Pfad vor dem Formular landet; die No-Contract-Demo | Design 1.2, 3.8 |
| Teil 5 | Formular-Residuum: Go-natives Formular, RJSF-Insel oder nur Editor | Design 3.6 |

### Vorregistrierte Entscheidungsregeln

Diese werden jetzt festgeschrieben, damit das Ergebnis später nicht
zurechtgebogen werden kann. `tally` in diesem Verzeichnis berechnet sie aus
einem CSV-Export der Antworten.

1. **Backlog-Reihenfolge.** Für jede Fehlerart k ist score(k) die Summe über
   alle Befragten von frequency(B1.k) x lateness(B2.k), mit den
   Häufigkeitsgewichten Nie 0, Ein- oder zweimal 1, Monatlich 2, Wöchentlich 4,
   Täglich 8 und den Gewichten für den Entdeckungszeitpunkt Vor dem Push 0.5,
   CI 1, Sync 2, Laufzeit 3, Kunde 4. Die Arten werden nach Score geordnet; die B3-Ränge sind
   der Tie-Break und ein Plausibilitätscheck (eine Art, die nach Score in den
   Top fünf liegt, aber in B3 nie genannt wird, wird als Diskrepanz gemeldet).
   Arten, die das Design offline nicht erkennen kann (B1.14), ordnen den
   Webhook-Vorhersage-Text, nicht das Check-Backlog.
2. **OpenShift-first bestätigt**, wenn OpenShift die relative Mehrheit der
   C1-Zeilen stellt oder mindestens 40 Prozent der Zeilen. Stellt eine andere
   Distribution die relative Mehrheit, wandert das MVP-Data-Tier-Profil zu
   dieser Distribution und OpenShift wird das erste Phase-2-Profil.
3. **GitLab-Adapter in den Monaten 5 bis 6**, wenn GitLab.com plus GitLab
   self-managed mehr als 30 Prozent der C4-Zeilen ausmachen, die einen Git-Host
   nennen (Zeilen mit der Antwort „kein Git-Repository“ werden aus dem Nenner
   ausgeschlossen und separat als „Übergabe“-Anteil gezählt, der in den
   Bot-Modus-Text einfließt).
4. **Plain-Reader zuerst**, wenn „CI-Pipeline führt helm upgrade aus“ plus
   „eine Person führt helm upgrade vom Laptop aus“ in C3 mehr sind als Argo CD
   plus Flux; andernfalls landen die Argo- und Flux-Reader zuerst und Helmfile
   folgt dem größeren von Helmfile und plain.
5. **Compose-Host-Snapshot** bleibt in Phase 2, es sei denn, „ja, in
   Produktion“ übersteigt 40 Prozent der C12-Zeilen; dann geht die offene
   Entscheidung 9 mit der Zahl an den Owner.
6. **Formular-Residuum (Teil 5).** Das Formular ist einen Go-nativen Renderer
   wert, wenn bei mindestens zwei von drei Teilnehmern der Formular-Arm weniger
   E3-Defekte oder eine mindestens 30 Prozent kürzere E1-Zeit hat und
   mindestens zwei von drei es in E5 bevorzugen. Gilt keines von beidem, wird
   das Formular zur RJSF-Insel aus Design 3.6. Gewinnt der Editor-Arm bei allen
   drei sowohl E3 als auch E5, verlässt das Formular das MVP.

### Stichprobe und Timing

- An jeden Consultant, Supporter, Entwickler und jede Plattform-Person in der
  Firma senden. Ein Ergebnis braucht mindestens 8 Antworten und mindestens 15
  Kundenzeilen; RESULT.md berichtet n in jedem Fall und markiert eine kleinere
  Stichprobe als „indikativ“.
- Nach zwei Wochen schließen (Ende Woche 3). Auswerten mit `go run ./cmd/tally
  responses.csv`, die Ausgabe in RESULT.md einfügen, den Tracker aktualisieren.
- Teil 5 läuft in den Wochen 5 bis 6, nachdem S9 den Prototyp geliefert hat;
  sein Ergebnis wird an RESULT.md angehängt.

### Exportformat für die Auswertung

Eine CSV-Zeile pro Befragtem, eine Spalte pro Fragen-ID, Kopfzeile mit den IDs.
Die Antworten der Matrixfragen verwenden exakt die obigen Spaltenbeschriftungen.
Mehrfachauswahl-Antworten werden mit `;` getrennt. Kundenspalten sind `C1.1`
bis `C13.5` (Frage.Kunde). B3 verwendet `B3.first`, `B3.second`, `B3.third` mit
der Nummer der Fehlerart. Die Freitextspalten werden wörtlich übernommen.
`testdata/responses.csv` in diesem Verzeichnis ist ein synthetisches Beispiel
mit der richtigen Form.
