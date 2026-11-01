# Jour 3 — Protection Edge (Global Load Balancer, Cloud Armor WAF & IAP), Cryptographie KMS CMEK & Journalisation SecOps Immuable

Au **Jour 2**, vous avez isolé les charges de travail de **TelP Secure Shop** dans une architecture **Shared VPC** segmentée (`subnet-front`, `subnet-back`, `subnet-bastion`), verrouillé le service `backend` en `--ingress=internal`, et placé vos 4 projets (`net`, `front`, `back`, `sec`) dans un périmètre **VPC Service Controls (`perim_student_${ACCOUNT_ID}`)**. Cependant, trois angles morts critiques subsistent encore :
1. **Exposition directe de la DMZ (`frontend`)** : Le service Cloud Run `frontend` est encore configuré en `--ingress=all`, dépourvu de pare-feu applicatif (WAF) contre les injections SQLi/XSS ou les attaques par déni de service, et sa route d'administration `/admin` n'est pas protégée par un proxy d'identité Zero-Trust.
2. **Gestion des secrets et des clés de chiffrement** : Le secret HMAC de signature des reçus (`RECEIPT_SIGNING_SECRET`) est encore injecté en clair dans une variable d'environnement Cloud Run (`INSECURE_DEFAULT_SECRET_2026`), et les reçus stockés dans `gs://${BACK_PROJECT_ID}-receipts` utilisent le chiffrement par défaut Google (GMEK) au lieu d'une clé **Customer-Managed Encryption Key (CMEK)** centralisée dans le projet de sécurité `$SEC_PROJECT_ID`.
3. **Absence de télémétrie d'audit centralisée et immuable** : Les journaux d'audit **Data Access** (`DATA_READ`, `DATA_WRITE`) ne sont pas activés sur vos services sensibles, aucun puits agrégé au niveau dossier (`folders/$FOLDER_ID`) n'exporte les traces vers un stockage **WORM (Write-Once-Read-Many)** verrouillé dans `$SEC_PROJECT_ID`, et aucune alerte SIEM ne détecte les modifications de politiques IAM (`SetIamPolicy`).

Au **Jour 3**, vous comblez ces trois angles morts à travers deux modules complémentaires :

1. **[Module 5 (`day3/mod5`) — Exposition Sécurisée, Global External Application Load Balancer, Cloud Armor WAF & Identity-Aware Proxy (IAP)](./mod5/README.md)** :
   - Création d'un **Serverless Network Endpoint Group (`neg-frontend`)** dans `$FRONT_PROJECT_ID` pointant vers le service Cloud Run `frontend`, attaché à deux **Backend Services globaux (`EXTERNAL_MANAGED`)** : `be-frontend` (trafic client général) et `be-admin` (portail d'administration `/admin` et `/admin/*`) routés par l'**URL Map `url-map-frontend`**.
   - Réservation d'une **adresse IPv4 statique globale (`lb-ip-frontend`)**, génération d'un certificat TLS pour `https://${LB_IP}.nip.io`, création du proxy HTTPS `https-proxy-frontend` et de la règle de transfert globale `fw-https-frontend` (`:443`), puis verrouillage de `frontend` en **`--ingress=internal-and-cloud-load-balancing`** afin d'interdire tout contournement direct de l'URL `*.run.app`.
   - Déploiement d'une politique **Cloud Armor (`waf-policy-frontend`)** attachée à `be-frontend` bloquant activement les injections SQL (`sqli-v33-stable` -> `403`), le Cross-Site Scripting (`xss-v33-stable` -> `403`) et limitant le débit par IP (`rate-based-ban` à `100 req/min` -> `429`), avec vérification expérimentale en direct.
   - Activation d'**Identity-Aware Proxy (IAP)** sur `be-admin`, autorisation du Service Agent IAP (`service-${FRONT_PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com`) à invoquer `frontend` (`roles/run.invoker`), et attribution du rôle `roles/iap.httpsResourceAccessor` selon le moindre privilège.

2. **[Module 6 (`day3/mod6`) — Gestion des Secrets, Chiffrement Cloud KMS CMEK Inter-Projets & Pipeline de Journalisation d'Audit Immuable (WORM & SIEM)](./mod6/README.md)** :
   - Migration du secret `RECEIPT_SIGNING_SECRET` vers **Secret Manager (`receipt-signing-secret`)** dans `$BACK_PROJECT_ID`, rotation vers une **version 2** active, délégation exclusive de `roles/secretmanager.secretAccessor` à `sa-backend`, et montage dynamique `--set-secrets` dans Cloud Run `backend`.
   - Création d'un trousseau **Cloud KMS (`kr-central`)** et d'une clé logicielle **`key-app-storage`** dans le projet dédié **`$SEC_PROJECT_ID`**, délégation inter-projets (`roles/cloudkms.cryptoKeyEncrypterDecrypter`) au Service Agent Cloud Storage de `$BACK_PROJECT_ID`, et chiffrement **CMEK par défaut** du bucket `gs://${BACK_PROJECT_ID}-receipts`.
   - Activation des journaux d'audit **Data Access (`ADMIN_READ`, `DATA_READ`, `DATA_WRITE`)** pour Cloud Storage, Cloud KMS et Secret Manager sur `$BACK_PROJECT_ID` et `$SEC_PROJECT_ID`, création d'un **puits de logs agrégé (`sink-folder-audit` avec `--include-children`)** sur `folders/$FOLDER_ID` vers `gs://${SEC_PROJECT_ID}-audit-logs`, et autorisation de l'identité du puits dans l'Access Level VPC-SC `al_student_${ACCOUNT_ID}` (en conservant impérativement votre compte étudiant et l'instructeur).
   - Verrouillage irréversible (**WORM `--lock-retention-policy`**, rétention `1d`) du bucket d'audit `gs://${SEC_PROJECT_ID}-audit-logs`, test d'immuabilité (`gcloud storage rm` rejeté en `403`), et création d'une métrique basée sur les logs (`iam-policy-change-count` sur `SetIamPolicy`) couplée à une **politique d'alerte Cloud Monitoring** active dans `$SEC_PROJECT_ID`.

---

## Cycle de Vie de la Journée 3 & Usage de `./bin/lab-ctl` (`setup`, `verify`, `cleanup`, `reset`)

1. **Au début du Module 5 (`setup`)** : Exécutez `source <(./bin/lab-ctl env)` puis `./bin/lab-ctl setup --day=3 --module=5` (qui active les APIs Compute, IAP et Certificate Manager sur `$FRONT_PROJECT_ID` et provisionne le Service Agent IAP).
2. **À la fin du Module 5 (`verify`, PAS de `cleanup` ni de `reset` !)** : Validez votre score `100 / 100` avec `./bin/lab-ctl verify --day=3 --module=5`, puis enchaînez **directement** sur le **Module 6** sans exécuter `cleanup --day=3` (le Load Balancer global déployé au Module 5 sert ainsi de point d'entrée HTTPS pour les tests E2E de signature des reçus au Module 6).
3. **Au début du Module 6 (`setup`)** : Exécutez `./bin/lab-ctl setup --day=3 --module=6` (qui active Secret Manager sur `$BACK_PROJECT_ID`, Cloud KMS / Logging / Monitoring / Storage sur `$SEC_PROJECT_ID`, et initialise le Service Agent GCS de `$BACK_PROJECT_ID`).
4. **En toute fin de Journée 3 uniquement (`cleanup --day=3` après le Module 6)** : Une fois le **Module 6** validé à **`100 / 100 points`** (`./bin/lab-ctl verify --day=3 --module=6`), exécutez :

```bash
./bin/lab-ctl cleanup --day=3
```

> **Pourquoi `cleanup --day=3` est-il indispensable en fin de Journée 3 ?**
> Les règles de transfert (**Global Forwarding Rules**) d'un Load Balancer global et les adresses IPv4 statiques réservées sont facturées à l'heure (~0,025 \$/h). Exécuté à la fin du Module 6, `./bin/lab-ctl cleanup --day=3` :
> 1. Rétablit d'abord l'ingress de `frontend` à `--ingress=all` (pour que l'application reste joignable au Jour 4 sans Load Balancer),
> 2. Supprime proprement les composants du Load Balancer global et libère l'IP statique `lb-ip-frontend` dans l'ordre inverse des dépendances,
> 3. S'assure que la VM `bastion` est arrêtée et que Cloud NAT est supprimé afin de préserver l'intégralité de vos crédits GCP entre le Jour 3 et le Jour 4.
