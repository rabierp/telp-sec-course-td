# Cybersécurité du Cloud (GCP) — Travaux Pratiques (Telecom Paris)

Ce dépôt (`rabierp/telp-sec-course-td`) contient le code de l'application fil
rouge **Cymbal Bank Serverless + AI**, l'outil de vérification `./bin/lab-ctl`
pour **Google Cloud Shell**, ainsi que les guides de travaux pratiques répartis
sur 4 demi-journées (8 modules).

## Architecture Fil Rouge (Landing Zone à 4 Projets par Étudiant)

Dans votre dossier dédié `Students/student-<slug>` sous l'organisation
`gcp.telecom-paris.fr` (région `europe-west1`), vous disposez de 4 projets
pré-créés :

1.  **`telp-<slug>-net` (`TELP_PROJECT_NET`)** : Projet hôte **Shared VPC**
    (`vpc-shared`), sous-réseaux segmentés (`subnet-front`, `subnet-back`,
    `subnet-bastion`), Cloud NAT, Private Cloud DNS et règles de pare-feu.
2.  **`telp-<slug>-front` (`TELP_PROJECT_FRONT`)** : Projet de service Shared
    VPC (DMZ publique) hébergeant le service Cloud Run `frontend`, le Global
    External Application Load Balancer, Cloud Armor (WAF/DDoS) et Identity-Aware
    Proxy (IAP).
3.  **`telp-<slug>-back` (`TELP_PROJECT_BACK`)** : Projet de service Shared VPC
    (Zone interne & IA) hébergeant les services Cloud Run internes `backend` et
    `ai-assistant`, le bastion `e2-micro` (`--no-address`), Firestore
    `(default)`, le bucket GCS des reçus, Secret Manager, Cloud DLP et Vertex
    AI + Model Armor.
4.  **`telp-<slug>-sec` (`TELP_PROJECT_SEC`)** : Projet SecOps centralisant les
    clés **Cloud KMS (CMEK)**, les clés de signature **Binary Authorization** et
    le bucket verrouillé des journaux d'audit (`sink-folder-audit`).

## Démarrage Rapide (Cloud Shell)

1.  Activez votre coupon de **50 \$ de crédits Google Cloud Education** reçu par
    e-mail avant d'ouvrir Cloud Shell.
2.  Clonez ce dépôt dans **Google Cloud Shell** :

    ```bash
    git clone https://github.com/rabierp/telp-sec-course-td.git
    cd telp-sec-course-td
    ```
3.  Au début de chaque session Cloud Shell, découvrez vos 4 projets et associez
    votre compte de facturation Education :

    ```bash
    source <(./bin/lab-ctl env)
    ```

## Programme des 4 Demi-Journées (8 Modules)

-   **Demi-journée 1 — Cloud Identity, Hiérarchie des Ressources & IAM**
    -   [Module 1 (`day1/mod1`)](./day1/mod1/README.md) : Hiérarchie des
        ressources, politiques d'organisation et moindre privilège IAM
        (`sa-frontend`, `sa-backend`, `sa-ai`).
    -   [Module 2 (`day1/mod2`)](./day1/mod2/README.md) : Impersonation sans clé
        JSON, IAM Conditions temporelles et Workload Identity Federation (WIF).
-   **Demi-journée 2 — Segmentation Réseau Shared VPC & Accès Zero-Trust**
    -   [Module 3 (`day2/mod3`)](./day2/mod3/README.md) : Shared VPC
        Host/Service (`net`, `front`, `back`), Private Google Access, Cloud NAT
        et ingress interne Cloud Run.
    -   [Module 4 (`day2/mod4`)](./day2/mod4/README.md) : Pare-feu VPC, Bastion
        `e2-micro` sans IP publique via IAP TCP Forwarding + OS Login, et VPC
        Service Controls (VPC-SC).
-   **Demi-journée 3 — Protection Edge, Cryptographie KMS & Journalisation
    SecOps**
    -   [Module 5 (`day3/mod5`)](./day3/mod5/README.md) : Global External
        Application Load Balancer, Cloud Armor (OWASP SQLi/XSS + DDoS Rate
        Limiting) et IAP.
    -   [Module 6 (`day3/mod6`)](./day3/mod6/README.md) : Cloud KMS centralisé
        (CMEK dans `sec`), Secret Manager et puits de logs d'audit agrégé au
        niveau du dossier étudiant.
-   **Demi-journée 4 — Protection des Données (DLP), Sécurité IA, Supply Chain &
    SCC**
    -   [Module 7 (`day4/mod7`)](./day4/mod7/README.md) : Sensitive Data
        Protection (Cloud DLP) et sécurisation de l'assistant Vertex AI
        (`gemini-2.5-flash`) avec Model Armor.
    -   [Module 8 (`day4/mod8`)](./day4/mod8/README.md) : Sécurité de la chaîne
        logistique (Binary Authorization avec attestations KMS) et Security
        Command Center (SCC).

## Commandes Utiles (`./bin/lab-ctl`)

```bash
# Découvrir vos 4 projets et exporter les variables d'environnement
source <(./bin/lab-ctl env)

# Rattraper automatiquement le pré-requis d'infrastructure d'un module
./bin/lab-ctl setup --day <1..4> --mod <1..8>

# Vérifier votre configuration de sécurité et enregistrer votre score
./bin/lab-ctl verify --day <1..4> --mod <1..8>

# Nettoyer les ressources facturées à l'heure en fin de demi-journée
./bin/lab-ctl cleanup --day <1..4>
```
