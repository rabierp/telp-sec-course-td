# Cybersécurité du Cloud (GCP) — Travaux Pratiques (Telecom Paris)

Ce dépôt (`rabierp/telp-sec-course-td`) contient le code de l'application fil
rouge **TelP Sovereign Shop**, le binaire de vérification `./bin/lab-ctl` pour
**Google Cloud Shell**, ainsi que les guides de travaux pratiques répartis sur
4 journées (8 modules).

## Architecture Fil Rouge (Landing Zone à 4 Projets par Étudiant)

Dans votre dossier dédié `folders/$FOLDER_ID` (`students/student-<slug>`) sous
l'organisation `gcp.telecom-paris.fr` (région `europe-west1`), vous disposez de
4 projets pré-créés :

1. **`telp-<slug>-net-<hash>` (`$NET_PROJECT_ID`)** : Projet hôte **Shared VPC**
   (`vpc-shared`), sous-réseaux segmentés (`subnet-front`, `subnet-back`,
   `subnet-bastion`), Private Google Access, Cloud Router + Cloud NAT (`nat-gw`),
   Private Cloud DNS (`zone-internal`) et règles de pare-feu.
2. **`telp-<slug>-front-<hash>` (`$FRONT_PROJECT_ID`)** : Projet de service
   Shared VPC (DMZ publique) hébergeant le service Cloud Run `frontend`, le
   Global External Application Load Balancer (`url-map-frontend`), Cloud Armor
   (`armor-frontend-waf`) et Identity-Aware Proxy (IAP sur `/admin`).
3. **`telp-<slug>-back-<hash>` (`$BACK_PROJECT_ID`)** : Projet de service Shared
   VPC (Zone interne & IA) hébergeant les services Cloud Run internes `backend`
   et `ai-assistant`, le bastion `e2-micro` (`bastion` sans IP publique),
   Firestore `(default)`, le bucket GCS des reçus (`gs://$BACK_PROJECT_ID-receipts`),
   Secret Manager (`receipt-signing-secret`), Cloud DLP (`dlp-inspect-pii`,
   `dlp-deid-pii`) et Model Armor (`telp-ai-guardrail`) + Vertex AI
   (`gemini-2.5-flash`).
4. **`telp-<slug>-sec-<hash>` (`$SEC_PROJECT_ID`)** : Projet SecOps centralisant
   les clés **Cloud KMS (CMEK)** (`kr-central/key-app-storage`), le bucket
   verrouillé des journaux d'audit (`gs://$SEC_PROJECT_ID-audit-logs` alimenté
   par le puits agrégé `sink-folder-audit`) et les alertes Cloud Monitoring.

## Démarrage Rapide (Cloud Shell)

1. Activez votre coupon de **50 \$ de crédits Google Cloud Education** reçu par
   e-mail avant d'ouvrir Cloud Shell.
2. Clonez ce dépôt dans **Google Cloud Shell** :

   ```bash
   git clone https://github.com/rabierp/telp-sec-course-td.git
   cd telp-sec-course-td
   ```

3. Au début de chaque session Cloud Shell, découvrez vos 4 projets et exportez
   les variables d'environnement (`FOLDER_ID`, `ACCESS_POLICY_ID`,
   `NET_PROJECT_ID`, `FRONT_PROJECT_ID`, `FRONT_PROJECT_NUMBER`,
   `BACK_PROJECT_ID`, `BACK_PROJECT_NUMBER`, `SEC_PROJECT_ID`, `REGION`) :

   ```bash
   source <(./bin/lab-ctl env)
   ```

## Programme des 4 Journées (8 Modules)

- **Jour 1 — Fondations GCP, Politiques d'Organisation & IAM Inter-Projets**
  - [Module 1 (`day1/mod1`)](./day1/mod1/README.md) : Découverte du dossier
    étudiant, facturation, politiques d'organisation (`europe-west1`, absence
    d'IP publique) et déploiement initial de *TelP Sovereign Shop* (`frontend`,
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

## Commandes Utiles (`./bin/lab-ctl`)

```bash
# Découvrir vos 4 projets et exporter les variables d'environnement
source <(./bin/lab-ctl env)

# Préparer ou rattraper automatiquement l'état initial d'un module
./bin/lab-ctl setup --day=<1..4> --module=<1..8>

# Vérifier votre configuration de sécurité et enregistrer votre score signé
./bin/lab-ctl verify --day=<1..4> --module=<1..8>

# Nettoyer les ressources facturées à l'heure en fin de journée
./bin/lab-ctl cleanup --day=<1..4>
```
