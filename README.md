# Cybersécurité du Cloud (GCP) — Travaux Pratiques (Telecom Paris)

Ce dépôt (`rabierp/telp-sec-course-td`) contient le code de l'application fil
rouge **TelP Secure Shop**, le binaire de vérification `./bin/lab-ctl` pour
**Google Cloud Shell**, ainsi que les guides de travaux pratiques répartis sur
4 journées (8 modules).

## Architecture Fil Rouge (Landing Zone à 4 Projets par Étudiant)

Dans votre dossier dédié `folders/$FOLDER_ID` (`Students/student-<slug>`) sous
l'organisation `gcp.telecom-paris.fr` (région `europe-west1`), vous disposez de
4 projets pré-créés :

1. **`telp-<slug>-net` (`$NET_PROJECT_ID`)** : Projet hôte **Shared VPC**
   (`vpc-shared`), sous-réseaux segmentés (`subnet-front`, `subnet-back`,
   `subnet-bastion`), Private Google Access, Cloud Router + Cloud NAT (`nat-gw`),
   Private Cloud DNS (`zone-internal`) et règles de pare-feu.
2. **`telp-<slug>-front` (`$FRONT_PROJECT_ID`)** : Projet de service
   Shared VPC (DMZ publique) hébergeant le service Cloud Run `frontend`, le
   Global External Application Load Balancer (`url-map-frontend`), Cloud Armor
   (`armor-frontend-waf`) et Identity-Aware Proxy (IAP sur `/admin`).
3. **`telp-<slug>-back` (`$BACK_PROJECT_ID`)** : Projet de service Shared
   VPC (Zone interne & IA) hébergeant les services Cloud Run internes `backend`
   et `ai-assistant`, le bastion `e2-micro` (`bastion` sans IP publique),
   Firestore `(default)`, le bucket GCS des reçus (`gs://$BACK_PROJECT_ID-receipts`),
   Secret Manager (`receipt-signing-secret`), Cloud DLP (`dlp-inspect-pii`,
   `dlp-deid-pii`) et Model Armor (`telp-ai-guardrail`) + Vertex AI
   (`gemini-2.5-flash`).
4. **`telp-<slug>-sec` (`$SEC_PROJECT_ID`)** : Projet SecOps centralisant
   les clés **Cloud KMS (CMEK)** (`kr-central/key-app-storage`), le bucket
   verrouillé des journaux d'audit (`gs://$SEC_PROJECT_ID-audit-logs` alimenté
   par le puits agrégé `sink-folder-audit`) et les alertes Cloud Monitoring.

## Démarrage Rapide (Cloud Shell)

1. Activez votre coupon de **50 \$ de crédits Google Cloud Education** reçu par
   e-mail avant d'ouvrir Cloud Shell.
2. Clonez ce dépôt dans **Google Cloud Shell** et initialisez vos identifiants Application Default Credentials (ADC) :

   ```bash
   git clone https://github.com/rabierp/telp-sec-course-td.git
   cd telp-sec-course-td
   gcloud auth application-default login
   ```

3. Au début de chaque session Cloud Shell, découvrez vos 4 projets et exportez
   les variables d'environnement (`FOLDER_ID`, `ACCESS_POLICY_ID`,
   `NET_PROJECT_ID`, `NET_PROJECT_NUMBER`, `FRONT_PROJECT_ID`,
   `FRONT_PROJECT_NUMBER`, `BACK_PROJECT_ID`, `BACK_PROJECT_NUMBER`,
   `SEC_PROJECT_ID`, `SEC_PROJECT_NUMBER`, `REGION`) :

   ```bash
   source <(./bin/lab-ctl env)
   ```

4. **Obtenir l'URL de votre application et la tester (`Cloud Shell Web Preview`, navigateur local ou `curl`)** :
   - **Récupérer les URLs Cloud Run (`FRONTEND_URL` et `BACKEND_URL`)** :
     ```bash
     export FRONTEND_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
     export BACKEND_URL="$(gcloud run services describe backend --project="${BACK_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
     echo "Frontend URL : ${FRONTEND_URL}"
     echo "Backend URL  : ${BACKEND_URL}"
     ```
     *(Pour `etudiant.test@gcp.telecom-paris.fr` : `FRONTEND_URL=https://frontend-2dg25e5jvq-ew.a.run.app` et `BACKEND_URL=https://backend-3oqe5uguta-ew.a.run.app`)*
   - **Pourquoi l'URL `.a.run.app` brute renvoie `403 Forbidden` dans un navigateur** : la politique d'organisation `constraints/iam.allowedPolicyMemberDomains` interdit `allUsers`, et Cloud Run attend un en-tête HTTP `Authorization: Bearer <ID_TOKEN>` (il ne déclenche pas de mire de connexion Google OAuth tant qu'Identity-Aware Proxy n'est pas configuré au Module 5).
   - **Accéder à l'application dans votre navigateur (Modules 1 à 4 & Jour 4)** : lancez le proxy d'authentification Cloud Run :
     ```bash
     gcloud run services proxy frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --port=8080
     ```
     Puis ouvrez :
     - **Depuis Google Cloud Shell** : cliquez sur le bouton **Web Preview** (en haut à droite) $\rightarrow$ **Preview on port 8080** (qui ouvre `https://8080-${WEB_HOST}/` et `https://8080-${WEB_HOST}/admin`).
     - **Depuis un terminal local** : ouvrez directement [http://localhost:8080/](http://localhost:8080/) (boutique) et [http://localhost:8080/admin](http://localhost:8080/admin) (portail d'administration listant les commandes Firestore).
   - **Accéder à l'application dans votre navigateur via IAP (Module 5)** : une fois le Global External Application Load Balancer et IAP activés :
     ```bash
     LB_IP="$(gcloud compute addresses describe ip-frontend-lb --global --project="${FRONT_PROJECT_ID}" --format="value(address)")"
     echo "Storefront : https://${LB_IP}.nip.io/"
     echo "Admin IAP  : https://${LB_IP}.nip.io/admin"
     ```
   - **En ligne de commande (`curl` direct sur `$FRONTEND_URL`)** :
     ```bash
     curl -sS -H "Authorization: Bearer $(gcloud auth print-identity-token)" "${FRONTEND_URL}/health"
     ```

## Programme des 4 Journées (8 Modules)

- **Jour 1 — Fondations GCP, Politiques d'Organisation & IAM Inter-Projets**
  - [Module 1 (`day1/mod1`)](./day1/mod1/README.md) : Découverte du dossier
    étudiant, facturation, politiques d'organisation (`europe-west1`, absence
    d'IP publique) et déploiement initial de *TelP Secure Shop* (`frontend`,
    `backend`, Firestore, GCS et `bastion`).
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
    périmètre VPC Service Controls (VPC-SC) englobant les 4 projets.
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
  - [Module 7 (`day4/mod7`)](./day4/mod7/README.md) : Souveraineté numérique,
    détection et remédiation des écarts de conformité Security Command Center
    (rotation KMS à 90 jours dans `sec`, UBLA + Versioning dans `back`, VPC Flow
    Logs sur `subnet-back` dans `net`).
  - [Module 8 (`day4/mod8`)](./day4/mod8/README.md) : Sécurisation du
    microservice `ai-assistant` (Vertex AI `gemini-2.5-flash`) avec Sensitive
    Data Protection (Cloud DLP `dlp-inspect-pii`, `dlp-deid-pii`) et Model Armor
    (`telp-ai-guardrail`).

## Tableau de Bord Personnel de Progression (Student Progress Dashboard)

Chaque étudiant ne voit **que ses propres journaux et résultats** (isolés par IAM sur un Managed Folder GCS dédié à votre compte `<votre-email>@gcp.telecom-paris.fr`). Chaque exécution de `./bin/lab-ctl` (`env`, `setup`, `verify`, `reset`, `cleanup`) met automatiquement à jour vos deux pages personnelles :

- **Page Principale (Résultat du dernier test ou de la dernière commande)** :
  `https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/<votre-email>/index.html`
  *(URL exacte exportée dans `$STUDENT_DASHBOARD_URL` par `source <(./bin/lab-ctl env)` ; ex. pour le compte de test : [https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/etudiant.test@gcp.telecom-paris.fr/index.html](https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/etudiant.test@gcp.telecom-paris.fr/index.html))*
- **Page Logs History (Historique complet de vos essais et commandes)** :
  `https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/<votre-email>/history.html`
  *(exportée dans `$STUDENT_LOGS_HISTORY_URL` ; ex. pour le compte de test : [https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/etudiant.test@gcp.telecom-paris.fr/history.html](https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/etudiant.test@gcp.telecom-paris.fr/history.html))*
- **Consultation depuis Cloud Shell (CLI)** :
  ```bash
  echo "Page principale : ${STUDENT_DASHBOARD_URL}"
  echo "Logs History    : ${STUDENT_LOGS_HISTORY_URL}"
  gcloud storage cat "gs://telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/DASHBOARD.md"
  ```

## Commandes Utiles (`./bin/lab-ctl`)

```bash
# Découvrir vos 4 projets et exporter les variables d'environnement
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
