# Cybersécurité du Cloud (GCP) — Travaux Pratiques (Telecom Paris)

Ce dépôt (`rabierp/telp-sec-course-td`) contient le code de l'application fil
rouge **TelP Secure Shop**, le binaire de vérification `./bin/lab-ctl` pour
**Google Cloud Shell**, ainsi que les guides de travaux pratiques répartis sur
4 journées (8 modules).

## Démarrage Rapide (Google Cloud Shell)

1. Activez votre coupon de **50 \$ de crédits Google Cloud Education** reçu par
   e-mail avant d'ouvrir Cloud Shell avec votre compte
   `<prenom.nom>@gcp.telecom-paris.fr`.
2. Clonez ce dépôt dans **Google Cloud Shell** :

   ```bash
   git clone https://github.com/rabierp/telp-sec-course-td.git
   cd telp-sec-course-td
   ```

   > **Note sur l'authentification** : Dans **Google Cloud Shell**, votre
   > session `gcloud` est déjà authentifiée automatiquement avec votre compte
   > `@gcp.telecom-paris.fr` (cliquez simplement sur **Autoriser** si Cloud
   > Shell vous le demande lors du premier appel). Vous n'avez **pas** besoin
   > d'exécuter `gcloud auth application-default login` dans Cloud Shell *(cette
   > commande n'est utile que si vous travaillez depuis un terminal local hors
   > de Cloud Shell)*.

3. Au début de chaque session Cloud Shell, initialisez et exportez vos variables
   d'environnement :

   ```bash
   source <(./bin/lab-ctl env)
   ```

## Programme des 4 Journées (8 Modules)

- **Jour 1 — Fondations GCP, Politiques d'Organisation & IAM Inter-Projets**
  - [Module 1 (`day1/mod1`)](./day1/mod1/README.md) : Découverte de l'architecture
    et du dossier étudiant, facturation, politiques d'organisation
    (`europe-west1`, absence d'IP publique) et déploiement initial de
    *TelP Secure Shop* (`frontend`, `backend`, Firestore, GCS et `bastion`).
  - [Module 2 (`day1/mod2`)](./day1/mod2/README.md) : Moindre privilège IAM,
    comptes de service dédiés (`sa-frontend`, `sa-backend`, `sa-bastion`),
    authentification OIDC inter-projets Cloud Run, Uniform Bucket-Level Access,
    zéro clé JSON et OS Login.
- **Jour 2 — Segmentation Réseau Shared VPC & Périmètre VPC Service Controls**
  - [Module 3 (`day2/mod3`)](./day2/mod3/README.md) : Architecture Shared VPC
    Host (`net`) & Service Projects (`front`, `back`), isolation IAM par
    sous-réseau (`subnet-front`, `subnet-back`, `subnet-bastion`), pare-feu IAP
    et Direct VPC Egress.
  - [Module 4 (`day2/mod4`)](./day2/mod4/README.md) : Private Google Access
    (PGA), Cloud NAT avec journalisation, Private Cloud DNS (`zone-internal`) et
    périmètre VPC Service Controls (VPC-SC) englobant vos projets.
- **Jour 3 — Protection Edge (WAF/IAP), Cryptographie KMS & Journalisation SecOps**
  - [Module 5 (`day3/mod5`)](./day3/mod5/README.md) : Global External
    Application Load Balancer, restriction d'ingress Cloud Run, Cloud Armor WAF
    (`sqli-v33-stable`, `xss-v33-stable`, limitation de débit) et IAP sur
    `/admin`.
  - [Module 6 (`day3/mod6`)](./day3/mod6/README.md) : Secret Manager
    (`receipt-signing-secret`), chiffrement Cloud KMS CMEK inter-projets
    (`sec` -> `back`), puits de logs d'audit agrégé sur `folders/$FOLDER_ID`
    (`sink-folder-audit`) vers un bucket WORM verrouillé et alertes SIEM.
- **Jour 4 — Conformité, Security Command Center (SCC) & Sécurité de l'IA (SAIF)**
  - [Module 7 (`day4/mod7`)](./day4/mod7/README.md) : Résidence des données en Europe,
    détection et remédiation des écarts de conformité Security Command Center
    (rotation KMS à 90 jours dans `sec`, UBLA + Versioning dans `back`, VPC Flow
    Logs sur `subnet-back` dans `net`).
  - [Module 8 (`day4/mod8`)](./day4/mod8/README.md) : Sécurisation du
    microservice `ai-assistant` (Vertex AI `gemini-2.5-flash`) avec Sensitive
    Data Protection (Cloud DLP `dlp-inspect-pii`, `dlp-deid-pii`) et Model Armor
    (`telp-ai-guardrail`).

## Tableau de Bord Personnel de Progression (Student Progress Dashboard)

Chaque étudiant ne voit **que ses propres journaux et résultats** (isolés par
IAM sur un Managed Folder GCS dédié à votre compte
`<votre-email>@gcp.telecom-paris.fr`). Chaque exécution de `./bin/lab-ctl`
(`env`, `setup`, `verify`, `reset`, `cleanup`) met automatiquement à jour vos
deux pages personnelles, dont les URLs exactes sont exportées par
`source <(./bin/lab-ctl env)` :

- **Page Principale (Résultat du dernier test ou de la dernière commande)** :
  Variable `$STUDENT_DASHBOARD_URL`
  (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/index.html`)
- **Page Logs History (Historique complet de vos essais et commandes)** :
  Variable `$STUDENT_LOGS_HISTORY_URL`
  (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/history.html`)
- **Afficher vos URLs personnelles ou consulter votre rapport depuis Cloud Shell** :

  ```bash
  echo "Page principale : ${STUDENT_DASHBOARD_URL}"
  echo "Logs History    : ${STUDENT_LOGS_HISTORY_URL}"
  gcloud storage cat "gs://telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/DASHBOARD.md"
  ```

## Commandes Utiles (`./bin/lab-ctl`)

```bash
# Découvrir votre environnement et exporter les variables du lab
source <(./bin/lab-ctl env)

# Préparer ou rattraper automatiquement l'état initial d'un module
./bin/lab-ctl setup --day=<1..4> --module=<1..8>

# Vérifier votre configuration de sécurité et enregistrer votre score signé
./bin/lab-ctl verify --day=<1..4> --module=<1..8>

# Réinitialiser les ressources et API d'un module à leur état pré-module
./bin/lab-ctl reset --day=<1..4> --module=<1..8>

# Nettoyer les ressources facturées à l'heure en fin de journée
./bin/lab-ctl cleanup --day=<1..4>
```
