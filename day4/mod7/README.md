# Jour 4 — Module 7 : Conformité Européenne, Security Command Center (SCC v2), Cloud Asset Inventory & Remédiation de Posture (CSPM)

## Objectifs Pédagogiques

Dans une architecture cloud multi-projets, la sécurité initiale au déploiement ne suffit pas : il faut auditer en continu la **résidence des données**, détecter les **dérives de configuration (*Configuration Drift*)** sur l'ensemble du dossier projet, et savoir articuler **détection asynchrone de conformité** et **inventaire d'actifs en temps réel**. Dans ce module, vous allez :
1. **Auditer la gouvernance effective et le périmètre de souveraineté (`folders/$FOLDER_ID`)** : Vérifier en lecture seule (`--effective`) l'héritage des politiques d'organisation sur votre dossier étudiant (`constraints/gcp.resourceLocations`, `constraints/iam.allowedPolicyMemberDomains`, `constraints/storage.uniformBucketLevelAccess`) et comprendre la frontière entre **résidence des données** et **souveraineté opérationnelle**.
2. **Interroger Security Command Center Standard v2 & Cloud Asset Inventory via le projet SecOps (`M7-04a`)** : Activer les APIs clientes `securitycenter.googleapis.com` et `cloudasset.googleapis.com` uniquement sur votre *quota project* de sécurité **`$SEC_PROJECT_ID`**, interroger les *findings* **SCC v2 (`--location=global`)** générés au niveau de l'Organisation par le framework intégré **`builtin-security-essentials`** de *Compliance Manager*, et comprendre pourquoi certaines règles sont vérifiées via **Cloud Asset Inventory**.
3. **Remédier à la dérive cryptographique Cloud KMS (`M7-01` — CIS Benchmark / ISO 27001)** : Inspecter la clé CMEK centrale `kr-central/key-app-storage` via Cloud Asset Inventory et lui appliquer une politique de rotation automatique **$\le 90$ jours (`7776000s`)** avec une prochaine échéance planifiée (`nextRotationTime`).
4. **Remédier aux dérives de stockage GCS (`M7-02`) et de visibilité réseau Shared VPC (`M7-03`)** : Corriger le bucket en dérive `gs://${BACK_PROJECT_ID}-compliance-drift` en activant **Uniform Bucket-Level Access (UBLA)** et **Object Versioning**, activer la télémétrie **VPC Flow Logs** sur le sous-réseau interne `subnet-back` dans `$NET_PROJECT_ID`, et valider de bout en bout la posture européenne de vos 4 projets (`M7-04b`, `M7-04c`).

---

## Étape 1 : Initialisation du Module 7 & Audit des Politiques d'Organisation Effectives

### 1.1 Charger l'environnement et préparer le laboratoire (`30 / 100` initial)

Depuis **Google Cloud Shell**, placez-vous à la racine du dépôt `telp-sec-course-td`, chargez vos variables d'environnement et initialisez le Module 7 :

```bash
source <(./bin/lab-ctl env)
./bin/lab-ctl setup --day=4 --module=7
```

La commande `setup` vérifie vos prérequis des Jours 2 et 3 sans écraser votre travail et déploie dans `$BACK_PROJECT_ID` un bucket hérité mal configuré (`gs://${BACK_PROJECT_ID}-compliance-drift`) présentant une dérive de conformité typique (ACL historiques activées et absence de versioning).

Exécutez immédiatement une première vérification :

```bash
./bin/lab-ctl verify --day=4 --module=7
```

> **Pourquoi le score initial est-il de `30 / 100` (et `0 / 4` contrôles complets) ?**
> `./bin/lab-ctl` évalue chaque sous-contrôle atomique de manière pondérée : vos ressources existantes respectent déjà la résidence `europe-west1` (`M7-02a` : 8 pts, `M7-04b` : 9 pts) et l'attachement de `subnet-back` au Shared VPC `vpc-shared` (`M7-03a` : 13 pts), soit `30 / 100`, mais aucun des 4 contrôles `M7-01`..`M7-04` n'est intégralement validé tant que les 3 dérives et l'accès CSPM ne sont pas corrigés.

### 1.2 Auditer les Organization Policies effectives sur `folders/${FOLDER_ID}` (Lecture seule)

Inspectez la politique **effective** héritée par votre dossier étudiant `folders/${FOLDER_ID}` sur trois contraintes structurantes :

```bash
gcloud resource-manager org-policies describe gcp.resourceLocations \
  --folder="${FOLDER_ID}" --effective

gcloud resource-manager org-policies describe iam.allowedPolicyMemberDomains \
  --folder="${FOLDER_ID}" --effective

gcloud resource-manager org-policies describe storage.uniformBucketLevelAccess \
  --folder="${FOLDER_ID}" --effective
```

Observez la différence entre les trois résultats :
- **`constraints/gcp.resourceLocations`** impose `in:europe-west1-locations` sur l'ensemble de vos 4 projets : toute création de ressource régionale hors de Belgique (`europe-west1`) est bloquée à la source par le plan de contrôle Resource Manager.
- **`constraints/iam.allowedPolicyMemberDomains`** restreint les membres IAM au seul domaine Google Workspace de l'école (`gcp.telecom-paris.fr`), empêchant l'ajout accidentel d'un compte externe `@gmail.com`.
- **`constraints/storage.uniformBucketLevelAccess`** n'est volontairement **pas** forcée au niveau du dossier `Students` afin de vous permettre d'observer et de corriger des dérives de configuration par bucket (`gs://${BACK_PROJECT_ID}-compliance-drift`).

> **Note de cours — Résidence des données vs Souveraineté opérationnelle (*Cloud Sovereign Solutions*) :**
> Dans cet environnement académique, nous mettons en œuvre la **résidence des données** (`gcp.resourceLocations`), le **chiffrement CMEK sous contrôle client** (`kr-central` dans `$SEC_PROJECT_ID`) et le **confinement de périmètre VPC-SC**. En production critique soumise à des exigences de souveraineté nationale ou sectorielle (SecNumCloud, OIV), Google Cloud complète ces contrôles techniques par des offres contractuelles et matérielles non activables sur un compte éducation standard : *Assured Workloads EU Data Boundary with Access Transparency / Access Approval* (approbation explicite de tout accès support Google), *External Key Manager (EKM / KAJ)* où la clé racine réside hors du cloud chez un tiers, ou encore un cloud physiquement et juridiquement opéré par une entité locale qualifiée (**S3NS PREMI3NS** en France, **Google Cloud Dedicated**).

---

## Étape 2 : Activer et Interroger Security Command Center v2 & Cloud Asset Inventory (`M7-04a`)

### 2.1 Pourquoi n'y a-t-il aucune « activation du tier SCC Standard » à effectuer ?

Dans l'architecture actuelle de Google Cloud (`2025+`), **Security Command Center (SCC) Standard** est **déjà actif nativement au niveau de l'Organisation** (`organizations/791737865143`) via le moteur ***Compliance Manager*** et son framework gratuit intégré **`builtin-security-essentials`** :
- Les résultats de conformité sont publiés automatiquement dans **SCC Findings v2** sous la source **`Compliance Evaluation Service`** (complétée par **`Sensitive Actions Service`** pour les actions IAM sensibles).
- Si vous interrogez `gcloud scc manage services list`, le module historique `SECURITY_HEALTH_ANALYTICS` (SHA) apparaît à `DISABLED` : c'est normal, car ce service hérité ne porte plus que les détecteurs **payants** des éditions *SCC Premium / Enterprise*.
- Vous n'avez donc **ni tier SCC à activer**, ni API à activer sur vos 4 projets : il suffit d'activer les deux APIs clientes **`securitycenter.googleapis.com`** et **`cloudasset.googleapis.com`** sur votre seul projet SecOps **`$SEC_PROJECT_ID`**, qui servira de ***quota / billing project*** pour interroger l'ensemble de votre dossier `folders/${FOLDER_ID}` !

Activez ces deux APIs sur **`$SEC_PROJECT_ID` uniquement** :

```bash
gcloud services enable \
  securitycenter.googleapis.com \
  cloudasset.googleapis.com \
  --project="${SEC_PROJECT_ID}"

echo "Attente de 20s pour la propagation de l'activation des APIs sur ${SEC_PROJECT_ID}..."
sleep 20
```

### 2.2 Interroger les *findings* SCC v2 sur votre dossier `folders/${FOLDER_ID}`

Exécutez la commande suivante pour lister les *findings* actifs sur l'ensemble de vos 4 projets enfants de `folders/${FOLDER_ID}` :

```bash
gcloud scc findings list "folders/${FOLDER_ID}" \
  --location=global \
  --billing-project="${SEC_PROJECT_ID}" \
  --filter='state="ACTIVE"' \
  --limit=30 \
  --format='table(finding.category,finding.resourceName,finding.severity,finding.eventTime)'
```

> **Pourquoi `--location=global` et `--billing-project="${SEC_PROJECT_ID}"` sont-ils tous les deux indispensables ?**
> 1. **`--location=global` (API SCC v2 vs v1 retirée et Data Residency SCC)** : Sans le drapeau `--location`, `gcloud scc findings list` appelle l'ancienne API **SCC v1**, désormais retirée pour les nouvelles organisations (`INVALID_ARGUMENT`). À l'inverse, passer `--location=eu` renvoie `INVALID_ARGUMENT: Request is prohibited by organization's policy (DRZ_LOCATION_MISMATCH)` tant qu'un dossier *Assured Workloads* avec résidence dédiée des métadonnées SCC n'est pas provisionné : les *findings* de l'organisation sont donc stockés et interrogés sur l'endpoint SCC v2 **`--location=global`**.
> 2. **`--billing-project="${SEC_PROJECT_ID}"` (Quota Project sur ressource Folder)** : Lorsqu'on interroge une ressource de niveau dossier (`folders/${FOLDER_ID}`), aucun `--project` n'est passé dans l'URI. Sans `--billing-project="${SEC_PROJECT_ID}"`, `gcloud` utiliserait le projet de quota par défaut exporté par `lab-ctl env` (`$BACK_PROJECT_ID`), sur lequel `securitycenter.googleapis.com` n'est pas activée (`SERVICE_DISABLED`) !

### 2.3 Pourquoi combiner SCC (`builtin-security-essentials`) et Cloud Asset Inventory ?

Le framework gratuit **`builtin-security-essentials`** comporte **44 règles essentielles** évaluées de manière **asynchrone** par *Compliance Evaluation Service*, dont :
- **`UNIFORM_BUCKET_LEVEL_ACCESS_ORG_POLICY`** (émis lorsqu'un bucket GCS n'impose pas *Uniform Bucket-Level Access*, comme `gs://${BACK_PROJECT_ID}-compliance-drift`),
- **`COMPUTE_VPC_ADVANCED_FLOW_LOGS_DISABLED`** (émis lorsqu'un sous-réseau VPC n'active pas les *VPC Flow Logs*, comme `subnet-back`).

En revanche, deux limites opérationnelles imposent de compléter SCC Standard par **Cloud Asset Inventory** et l'inspection directe des ressources :
1. **Périmètre des règles gratuites vs Premium** : La détection automatique de l'absence de rotation des clés KMS (`KMS_KEY_NOT_ROTATED`) et de l'absence de versioning GCS (`BUCKET_VERSIONING_DISABLED`) fait partie des modules payants *Security Health Analytics* (SCC Premium / Enterprise), absents de `builtin-security-essentials`.
2. **Latence du scan asynchrone** : Un moteur CSPM par *findings* met plusieurs minutes à plusieurs heures pour ré-évaluer un parc après création ou correction d'une ressource, alors que **Cloud Asset Inventory** permet d'auditer l'état réel des configurations (`rotationPeriod`, `uniformBucketLevelAccess`, `enableFlowLogs`) **instantanément** à l'échelle de tout votre dossier `folders/${FOLDER_ID}`.

Vérifiez-le en listant en temps réel toutes les clés cryptographiques KMS et tous les buckets GCS de votre dossier `folders/${FOLDER_ID}` via Cloud Asset Inventory :

```bash
gcloud asset list \
  --folder="${FOLDER_ID}" \
  --asset-types="cloudkms.googleapis.com/CryptoKey,storage.googleapis.com/Bucket,compute.googleapis.com/Subnetwork" \
  --billing-project="${SEC_PROJECT_ID}" \
  --format="table(assetType,name)"
```

---

## Étape 3 : Remédiation `M7-01` — Rotation Automatique $\le 90$ Jours sur Cloud KMS (`25 pts`)

### 3.1 Inspecter l'absence de calendrier de rotation sur `kr-central/key-app-storage`

Au Module 6, vous avez créé la clé CMEK `key-app-storage` dans `$SEC_PROJECT_ID` pour chiffrer `gs://${BACK_PROJECT_ID}-receipts`, mais sans planifier de rotation périodique. Vérifiez ses métadonnées actuelles via Cloud Asset Inventory / KMS :

```bash
gcloud kms keys describe key-app-storage \
  --keyring=kr-central \
  --location="${REGION}" \
  --project="${SEC_PROJECT_ID}" \
  --billing-project="${SEC_PROJECT_ID}" \
  --format="yaml(name,purpose,rotationPeriod,nextRotationTime)"
```

Vous constatez que `rotationPeriod` et `nextRotationTime` sont vides. Or le **CIS Google Cloud Computing Foundations Benchmark** et **ISO 27001** exigent une rotation automatique au maximum tous les **90 jours (`7776000s`)** afin de limiter le volume de données chiffrées sous une même version de clé (*cryptographic key wear-out*).

### 3.2 Configurer une rotation automatique à 90 jours (`7776000s`)

Appliquez une période de rotation de `7776000s` (90 jours exacts) et planifiez la prochaine rotation à `+30 jours` en format UTC RFC3339 :

```bash
NEXT_ROTATION="$(date -u -d '+30 days' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -v+30d +%Y-%m-%dT%H:%M:%SZ)"
gcloud kms keys update key-app-storage \
  --keyring=kr-central \
  --location="${REGION}" \
  --project="${SEC_PROJECT_ID}" \
  --billing-project="${SEC_PROJECT_ID}" \
  --rotation-period="7776000s" \
  --next-rotation-time="${NEXT_ROTATION}"
```

> **Comment fonctionne la rotation CMEK dans GCS sans interruption de service ?** Lors de chaque rotation, Cloud KMS génère une nouvelle version primaire (`v2`, `v3`, ...) utilisée pour tous les **nouveaux** objets écrits dans `gs://${BACK_PROJECT_ID}-receipts`, tout en conservant les versions précédentes actives pour déchiffrer de manière transparente les objets existants.

---

## Étape 4 : Remédiation `M7-02` — Durcissement du Bucket en Dérive (UBLA & Versioning) (`25 pts`)

### 4.1 Constater la dérive sur `gs://${BACK_PROJECT_ID}-compliance-drift`

Inspectez la configuration actuelle du bucket `gs://${BACK_PROJECT_ID}-compliance-drift` dans `$BACK_PROJECT_ID` :

```bash
gcloud storage buckets describe "gs://${BACK_PROJECT_ID}-compliance-drift" \
  --project="${BACK_PROJECT_ID}" \
  --format="yaml(name,location,uniform_bucket_level_access,versioning_enabled)"
```

Deux vulnérabilités sont présentes :
1. **`uniform_bucket_level_access: false`** (déclenchant la catégorie SCC `UNIFORM_BUCKET_LEVEL_ACCESS_ORG_POLICY`) : les ACL POSIX historiques par objet sont actives, ce qui permet d'accorder des droits sur un objet individuel en contournant la politique IAM centralisée du bucket et complique tout audit IAM.
2. **`versioning_enabled: false`** : en cas d'écrasement accidentel ou de compromission applicative (*ransomware* / suppression logique), les objets détruits sont définitivement perdus.

### 4.2 Activer Uniform Bucket-Level Access (UBLA) et Object Versioning

Corrigez simultanément ces deux paramètres sur `gs://${BACK_PROJECT_ID}-compliance-drift` tout en conservant sa localisation `EUROPE-WEST1` :

```bash
gcloud storage buckets update "gs://${BACK_PROJECT_ID}-compliance-drift" \
  --project="${BACK_PROJECT_ID}" \
  --uniform-bucket-level-access \
  --versioning
```

---

## Étape 5 : Remédiation `M7-03` — Télémétrie VPC Flow Logs sur `subnet-back` (`25 pts`)

### 5.1 Constater l'absence de télémétrie réseau sur `subnet-back`

Dans le projet hôte Shared VPC **`$NET_PROJECT_ID`**, inspectez la configuration du sous-réseau interne de données `subnet-back` :

```bash
gcloud compute networks subnets describe subnet-back \
  --project="${NET_PROJECT_ID}" \
  --billing-project="${NET_PROJECT_ID}" \
  --region="${REGION}" \
  --format="yaml(name,ipCidrRange,enableFlowLogs,logConfig)"
```

`enableFlowLogs` est absent (`false`), ce qui déclenche la règle `COMPUTE_VPC_ADVANCED_FLOW_LOGS_DISABLED` de `builtin-security-essentials` : en cas de mouvement latéral Est-Ouest depuis un conteneur compromis ou depuis la VM `bastion`, l'équipe SecOps ne dispose d'aucune trace IP/port dans Cloud Logging.

### 5.2 Activer les VPC Flow Logs avec échantillonnage maîtrisé (`0.1`)

Activez les *VPC Flow Logs* sur `subnet-back` avec un taux d'échantillonnage de `10 %` (`--logging-flow-sampling=0.1`, recommandé en ingénierie SecOps pour concilier visibilité forensique et maîtrise du volume d'ingestion Cloud Logging) :

```bash
gcloud compute networks subnets update subnet-back \
  --project="${NET_PROJECT_ID}" \
  --billing-project="${NET_PROJECT_ID}" \
  --region="${REGION}" \
  --enable-flow-logs \
  --logging-flow-sampling=0.1
```

---

## Étape 6 : Validation Finale du Module 7 (`100 / 100`)

Lancez la vérification complète du Module 7 :

```bash
./bin/lab-ctl verify --day=4 --module=7
```

Vérifiez que les 4 contrôles (`M7-01`, `M7-02`, `M7-03` et `M7-04`) affichent **`[OK]`** pour un score total de **`100 / 100 points`**, et consultez la mise à jour sur votre tableau de bord personnel (`echo "${STUDENT_DASHBOARD_URL}"`).

> **Important : Ne lancez PAS `cleanup` ni `reset` maintenant !** Passez directement au **[Module 8 (`day4/mod8`)](../mod8/README.md)** pour sécuriser le microservice d'IA générative `ai-assistant` de *TelP Secure Shop*.
