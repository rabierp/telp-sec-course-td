# Jour 3 — Module 6 : Gestion des Secrets (Secret Manager), Chiffrement Cloud KMS CMEK Inter-Projets & Pipeline de Journalisation Immuable (WORM & SIEM)

## Objectifs Pédagogiques

Au Module 5, vous avez sécurisé l'exposition externe de `frontend` derrière un Global External Application Load Balancer protégé par Cloud Armor WAF et Identity-Aware Proxy (IAP). Il vous reste à sécuriser **les secrets applicatifs, le chiffrement des données au repos et la traçabilité d'audit immuable** :
1. **Élimination des secrets en clair & Rotation avec Secret Manager (`M6-01`)** : Constater que le secret de signature HMAC des reçus (`RECEIPT_SIGNING_SECRET`) est actuellement exposé en clair dans la configuration du service Cloud Run `backend` (`INSECURE_DEFAULT_SECRET_2026`), créer le secret **`receipt-signing-secret`** dans **Secret Manager** (`$BACK_PROJECT_ID`), effectuer une **rotation vers une version 2**, accorder exclusivement `roles/secretmanager.secretAccessor` à `sa-backend`, et monter dynamiquement `receipt-signing-secret:latest` via `--set-secrets`.
2. **Séparation des Pouvoirs & Chiffrement Cloud KMS CMEK Inter-Projets (`M6-02`)** : Créer un trousseau **Cloud KMS (`kr-central`)** et une clé logicielle **`key-app-storage`** dans le projet de sécurité dédié **`$SEC_PROJECT_ID`**, autoriser le **Service Agent Cloud Storage** de `$BACK_PROJECT_ID` (`service-${BACK_PROJECT_NUMBER}@gs-project-accounts.iam.gserviceaccount.com`) à chiffrer/déchiffrer avec cette clé (`roles/cloudkms.cryptoKeyEncrypterDecrypter`), configurer `key-app-storage` comme clé **CMEK par défaut** du bucket `gs://${BACK_PROJECT_ID}-receipts`, et renseigner `KMS_KEY_NAME` sur `backend`.
3. **Journaux d'Audit Data Access, Puits Agrégé de Dossier & Ingression VPC-SC (`M6-03`)** : Activer les journaux d'audit **Data Access (`ADMIN_READ`, `DATA_READ`, `DATA_WRITE`)** pour `storage.googleapis.com`, `cloudkms.googleapis.com` et `secretmanager.googleapis.com` sur les projets `$BACK_PROJECT_ID` et `$SEC_PROJECT_ID`, créer un **puits de logs agrégé (`sink-folder-audit` avec `--include-children`)** sur votre dossier `folders/$FOLDER_ID` vers `gs://${SEC_PROJECT_ID}-audit-logs`, et autoriser l'identité du puits (`writerIdentity`) dans l'Access Level VPC-SC **`al_student_${ACCOUNT_ID}`** tout en conservant votre compte étudiant et celui de l'instructeur.
4. **Stockage d'Audit Immuable WORM & Détection SIEM Cloud Monitoring (`M6-04`)** : Appliquer et **verrouiller irréversiblement (`--lock-retention-period`)** une politique de rétention WORM de `1d` (`86400s`) sur `gs://${SEC_PROJECT_ID}-audit-logs`, prouver expérimentalement qu'aucun administrateur ne peut supprimer un journal d'audit avant l'expiration de sa rétention (`HTTP 403`), puis créer la métrique basée sur les logs **`iam-policy-change-count`** (`SetIamPolicy`) et une **politique d'alerte Cloud Monitoring** active dans `$SEC_PROJECT_ID`.

---

## Étape 1 : Initialisation de l'Environnement et État des Lieux (`0 / 100`)

Depuis **Google Cloud Shell**, placez-vous à la racine du dépôt `telp-sec-course-td`, chargez vos variables d'environnement et préparez le Module 6 :

```bash
source <(./bin/lab-ctl env)
./bin/lab-ctl setup --day=3 --module=6
```

Lancez une première vérification pour observer les 4 contrôles initialement en échec (`0 / 100 points`) :

```bash
./bin/lab-ctl verify --day=3 --module=6
```

---

## Étape 2 : Migration vers Secret Manager, Rotation (`v2`) & Montage Cloud Run (`M6-01` — 25 pts)

### 2.1 Constat avant remédiation : le secret HMAC est visible en clair dans la définition Cloud Run

Inspectez les variables d'environnement du service Cloud Run `backend` dans `$BACK_PROJECT_ID` :

```bash
gcloud run services describe backend \
  --project="${BACK_PROJECT_ID}" \
  --region="${REGION}" \
  --format="yaml(spec.template.spec.containers[0].env)"
```

Vous constatez que `RECEIPT_SIGNING_SECRET` est stocké en texte clair (`value: INSECURE_DEFAULT_SECRET_2026`) : tout lecteur disposant de `roles/viewer` ou `roles/run.viewer` sur le projet peut lire ce secret et forger de faux reçus signés.

### 2.2 Créer le secret `receipt-signing-secret`, effectuer une rotation (`v1` -> `v2`) et déléguer l'accès IAM à `sa-backend`

Dans `$BACK_PROJECT_ID`, créez le secret `receipt-signing-secret` avec une politique de réplication régionale gérée (`--replication-policy="user-managed" --locations="${REGION}"` — imposée par la contrainte d'Organization Policy de souveraineté européenne `constraints/gcp.resourceLocations` = `in:eu-locations` que vous avez appliquée au Jour 1 Module 1, qui interdit `--replication-policy="automatic"` global), ajoutez une première version (`v1`) puis effectuez immédiatement une rotation vers une seconde version (`v2`), et accordez **`roles/secretmanager.secretAccessor`** exclusivement au compte de service `sa-backend@${BACK_PROJECT_ID}.iam.gserviceaccount.com` :

```bash
gcloud secrets create receipt-signing-secret \
  --project="${BACK_PROJECT_ID}" \
  --replication-policy="user-managed" \
  --locations="${REGION}"

printf "telp-hmac-v1-%s" "${ACCOUNT_ID}" | gcloud secrets versions add receipt-signing-secret \
  --project="${BACK_PROJECT_ID}" \
  --data-file=-

printf "telp-hmac-v2-rotated-%s" "${ACCOUNT_ID}" | gcloud secrets versions add receipt-signing-secret \
  --project="${BACK_PROJECT_ID}" \
  --data-file=-

gcloud secrets add-iam-policy-binding receipt-signing-secret \
  --project="${BACK_PROJECT_ID}" \
  --member="serviceAccount:sa-backend@${BACK_PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"

gcloud run services update backend \
  --project="${BACK_PROJECT_ID}" \
  --region="${REGION}" \
  --remove-env-vars="RECEIPT_SIGNING_SECRET" \
  --set-secrets="RECEIPT_SIGNING_SECRET=receipt-signing-secret:latest"
```

> **Pourquoi `--remove-env-vars="RECEIPT_SIGNING_SECRET"` est-il requis ?** Cloud Run distingue le type d'une variable d'environnement littérale (`value`) d'une variable montée depuis Secret Manager (`valueSource.secretKeyRef`). Il faut donc supprimer la variable en clair existante lors de son remplacement par `--set-secrets`.

---

## Étape 3 : Chiffrement Cloud KMS CMEK Inter-Projets (`$SEC_PROJECT_ID` -> `$BACK_PROJECT_ID`) (`M6-02` — 25 pts)

Pourquoi héberger la clé KMS dans **`$SEC_PROJECT_ID`** alors que le bucket `gs://${BACK_PROJECT_ID}-receipts` se trouve dans **`$BACK_PROJECT_ID`** ?
Ce modèle met en œuvre la **séparation des pouvoirs cryptographique (Separation of Duties)** : un administrateur du projet applicatif `$BACK_PROJECT_ID` ne peut ni détruire la clé maître KMS, ni modifier sa politique IAM, et l'équipe Sécurité (`$SEC_PROJECT_ID`) conserve la capacité de révoquer instantanément l'accès aux données en cas d'incident.

### 3.1 Créer le KeyRing `kr-central` et la CryptoKey logicielle `key-app-storage` dans `$SEC_PROJECT_ID`

> **Attention (`--protection-level=software`)** : Utilisez impérativement `--protection-level=software` (inclus dans le quota gratuit / quelques centimes par mois) et **jamais** `hsm`, afin de préserver votre crédit de 50 \$.

```bash
gcloud kms keyrings create kr-central \
  --location="${REGION}" \
  --project="${SEC_PROJECT_ID}"

gcloud kms keys create key-app-storage \
  --keyring=kr-central \
  --location="${REGION}" \
  --purpose=encryption \
  --protection-level=software \
  --project="${SEC_PROJECT_ID}"

export KMS_KEY_URI="projects/${SEC_PROJECT_ID}/locations/${REGION}/keyRings/kr-central/cryptoKeys/key-app-storage"
echo "KMS_KEY_URI = ${KMS_KEY_URI}"
```

### 3.2 Autoriser le Service Agent Cloud Storage de `$BACK_PROJECT_ID` sur la clé KMS et activer CMEK sur le bucket de reçus

Lorsqu'un objet est écrit dans `gs://${BACK_PROJECT_ID}-receipts`, ce n'est pas `sa-backend` qui appelle directement Cloud KMS pour chiffrer les blocs de données au repos, mais le **Service Agent Google Cloud Storage** du projet `$BACK_PROJECT_ID` (`service-${BACK_PROJECT_NUMBER}@gs-project-accounts.iam.gserviceaccount.com`). Accordez-lui `roles/cloudkms.cryptoKeyEncrypterDecrypter` sur `key-app-storage`, puis configurez `key-app-storage` comme clé CMEK par défaut du bucket :

```bash
export GCS_SA="service-${BACK_PROJECT_NUMBER}@gs-project-accounts.iam.gserviceaccount.com"

gcloud kms keys add-iam-policy-binding key-app-storage \
  --keyring=kr-central \
  --location="${REGION}" \
  --project="${SEC_PROJECT_ID}" \
  --member="serviceAccount:${GCS_SA}" \
  --role="roles/cloudkms.cryptoKeyEncrypterDecrypter"

gcloud storage buckets update "gs://${BACK_PROJECT_ID}-receipts" \
  --default-encryption-key="${KMS_KEY_URI}" \
  --project="${BACK_PROJECT_ID}"
```

### 3.3 Mettre à jour `backend` (`KMS_KEY_NAME`) et vérifier la signature et le chiffrement CMEK d'un reçu

Mettez maintenant à jour le service Cloud Run `backend` pour injecter `KMS_KEY_NAME=${KMS_KEY_URI}` (avec `--update-env-vars` afin de préserver les autres variables existantes) :

```bash
gcloud run services update backend \
  --project="${BACK_PROJECT_ID}" \
  --region="${REGION}" \
  --update-env-vars="KMS_KEY_NAME=${KMS_KEY_URI}"
```

Vérifiez maintenant que `M6-01` et `M6-02` passent (`50 / 100 points`) :

```bash
./bin/lab-ctl verify --day=3 --module=6
```

---

## Étape 4 : Journaux d'Audit Data Access, Bucket WORM & Puits Agrégé de Dossier (`M6-03` — 25 pts)

Par défaut, Google Cloud n'enregistre que les journaux d'audit **Admin Activity** (création/suppression de ressources, modifications IAM), mais **pas** les journaux **Data Access** (`DATA_READ`, `DATA_WRITE`) qui tracent qui a lu un secret dans Secret Manager, qui a utilisé une clé Cloud KMS ou qui a lu/écrit un objet dans Cloud Storage.

> **Pourquoi configurer `auditConfigs` au niveau des projets `$BACK_PROJECT_ID` et `$SEC_PROJECT_ID` ?**
> Conformément au modèle de gouvernance moindre privilège mis en place au Jour 1, votre compte étudiant dispose de `roles/logging.configWriter` sur `folders/${FOLDER_ID}` (pour créer le puits agrégé `sink-folder-audit`), mais **pas** de `roles/resourcemanager.folderIamAdmin` (pour vous empêcher d'altérer vos propres droits IAM sur le dossier). En revanche, vous êtes `roles/owner` de vos 4 projets : vous activez donc les `auditConfigs` Data Access directement dans la politique IAM des projets **`$BACK_PROJECT_ID`** et **`$SEC_PROJECT_ID`**, et le puits de dossier `sink-folder-audit` (`--include-children`) collecte automatiquement l'ensemble des journaux d'audit de vos 4 projets enfants !

### 4.1 Activer les journaux d'audit Data Access (`ADMIN_READ`, `DATA_READ`, `DATA_WRITE`) sur `$BACK_PROJECT_ID` et `$SEC_PROJECT_ID`

Utilisez `python3` pour ajouter la section `auditConfigs` aux politiques IAM de `$BACK_PROJECT_ID` et `$SEC_PROJECT_ID` sans écraser les `bindings` existants :

```bash
TMP_AUDIT="$(mktemp -d)"
for PROJ in "${BACK_PROJECT_ID}" "${SEC_PROJECT_ID}"; do
  gcloud projects get-iam-policy "${PROJ}" --format=json > "${TMP_AUDIT}/iam-${PROJ}.json"
  python3 -c '
import json, sys
p = json.load(open(sys.argv[1]))
p["auditConfigs"] = [
  {"service": "storage.googleapis.com", "auditLogConfigs": [{"logType": "ADMIN_READ"}, {"logType": "DATA_READ"}, {"logType": "DATA_WRITE"}]},
  {"service": "cloudkms.googleapis.com", "auditLogConfigs": [{"logType": "ADMIN_READ"}, {"logType": "DATA_READ"}, {"logType": "DATA_WRITE"}]},
  {"service": "secretmanager.googleapis.com", "auditLogConfigs": [{"logType": "ADMIN_READ"}, {"logType": "DATA_READ"}, {"logType": "DATA_WRITE"}]}
]
json.dump(p, open(sys.argv[2], "w"), indent=2)
' "${TMP_AUDIT}/iam-${PROJ}.json" "${TMP_AUDIT}/iam-${PROJ}-updated.json"
  gcloud projects set-iam-policy "${PROJ}" "${TMP_AUDIT}/iam-${PROJ}-updated.json" >/dev/null
  echo "auditConfigs Data Access activés sur ${PROJ}"
done
rm -rf "${TMP_AUDIT}"
```

### 4.2 Créer le bucket d'audit `gs://${SEC_PROJECT_ID}-audit-logs` dans `$SEC_PROJECT_ID`

```bash
gcloud storage buckets create "gs://${SEC_PROJECT_ID}-audit-logs" \
  --project="${SEC_PROJECT_ID}" \
  --location="${REGION}" \
  --uniform-bucket-level-access
```

### 4.3 Créer le puits agrégé `sink-folder-audit` (`--include-children`) sur `folders/${FOLDER_ID}` et autoriser son `writerIdentity`

Créez le puits de logs agrégé `sink-folder-audit` au niveau de votre dossier `folders/${FOLDER_ID}` avec `--include-children`, récupérez son identité dédiée (`writerIdentity` : `serviceAccount:service-folder-${FOLDER_ID}@gcp-sa-logging.iam.gserviceaccount.com`), et accordez-lui `roles/storage.objectCreator` sur le bucket d'audit :

```bash
gcloud logging sinks create sink-folder-audit \
  "storage.googleapis.com/${SEC_PROJECT_ID}-audit-logs" \
  --folder="${FOLDER_ID}" \
  --include-children \
  --log-filter='logName:"logs/cloudaudit.googleapis.com"'

export SINK_SA="$(gcloud logging sinks describe sink-folder-audit --folder="${FOLDER_ID}" --format='value(writerIdentity)')"
echo "SINK_SA = ${SINK_SA}"

gcloud storage buckets add-iam-policy-binding "gs://${SEC_PROJECT_ID}-audit-logs" \
  --member="${SINK_SA}" \
  --role="roles/storage.objectCreator" \
  --project="${SEC_PROJECT_ID}"
```

### 4.4 Autoriser `SINK_SA` dans l'Access Level VPC-SC `al_student_${ACCOUNT_ID}` (Interaction VPC-SC / Folder Sink)

Comme vos 4 projets sont protégés par le périmètre **VPC Service Controls (`perim_student_${ACCOUNT_ID}`)** créé au Module 4 et que le puits `sink-folder-audit` réside au niveau du dossier `folders/${FOLDER_ID}`, toute écriture de `SINK_SA` vers `storage.googleapis.com/${SEC_PROJECT_ID}-audit-logs` serait bloquée par VPC-SC si `SINK_SA` n'était pas autorisé dans votre Access Level `al_student_${ACCOUNT_ID}` !

> **Attention critique — Conservez les 3 principaux dans `al_student_${ACCOUNT_ID}` !**
> Lorsque vous mettez à jour `al_student_${ACCOUNT_ID}`, le fichier YAML **remplace** la liste des membres. Vous devez impérativement inclure **les 3 principaux** :
> 1. `user:${STUDENT_EMAIL}` (pour ne pas vous verrouiller vous-même hors de votre périmètre VPC-SC !),
> 2. `user:pascal.rabier@gcp.telecom-paris.fr` (pour permettre l'audit instructeur),
> 3. `${SINK_SA}` (`serviceAccount:service-folder-${FOLDER_ID}@gcp-sa-logging.iam.gserviceaccount.com`).

```bash
TMP_AL="$(mktemp -d)"
cat <<EOF > "${TMP_AL}/al-spec.yaml"
- members:
  - user:${STUDENT_EMAIL}
  - user:pascal.rabier@gcp.telecom-paris.fr
  - ${SINK_SA}
EOF

gcloud access-context-manager levels update "al_student_${ACCOUNT_ID}" \
  --policy="${ACCESS_POLICY_ID}" \
  --basic-level-spec="${TMP_AL}/al-spec.yaml" \
  --combine-function=AND
rm -rf "${TMP_AL}"
```

---

## Étape 5 : Verrouillage Irréversible WORM du Bucket d'Audit & Alerte SIEM (`M6-04` — 25 pts)

### 5.1 Configurer et verrouiller (`--lock-retention-period`) la rétention WORM (`1d`) sur `gs://${SEC_PROJECT_ID}-audit-logs`

Appliquez une période de rétention minimale de **1 jour (`1d` = `86400s`)** sur `gs://${SEC_PROJECT_ID}-audit-logs`, puis **verrouillez irréversiblement** cette politique de rétention (`--lock-retention-period`) en confirmant avec `y` *(attention : n'utilisez pas `--quiet` sur `--lock-retention-period`, car la réponse par défaut de l'invite de sécurité est `N`, ce qui annulerait le verrouillage)* :

```bash
gcloud storage buckets update "gs://${SEC_PROJECT_ID}-audit-logs" \
  --retention-period=1d \
  --project="${SEC_PROJECT_ID}"

printf "y\n" | gcloud storage buckets update "gs://${SEC_PROJECT_ID}-audit-logs" \
  --lock-retention-period \
  --project="${SEC_PROJECT_ID}"
```

### 5.2 Preuve expérimentale d'immuabilité WORM : même `roles/owner` ne peut pas supprimer un journal d'audit (`HTTP 403`)

Écrivez un enregistrement de test dans `gs://${SEC_PROJECT_ID}-audit-logs`, puis tentez immédiatement de le supprimer avec votre compte (qui détient pourtant `roles/owner` sur `$SEC_PROJECT_ID`) :

```bash
echo '{"event":"worm_immutability_test"}' | gcloud storage cp - "gs://${SEC_PROJECT_ID}-audit-logs/worm-test.json" --project="${SEC_PROJECT_ID}"

echo "Tentative de suppression d'un journal protégé par Bucket Lock WORM (doit être rejetée en 403) :"
gcloud storage rm "gs://${SEC_PROJECT_ID}-audit-logs/worm-test.json" --project="${SEC_PROJECT_ID}" || true
```

Vous observez `HTTPError 403: Object ... is subject to bucket's retention policy and cannot be deleted or overwritten` : un attaquant qui compromettrait un compte administrateur ne peut pas effacer ses traces d'intrusion.

### 5.3 Créer la métrique basée sur les logs `iam-policy-change-count` et la politique d'alerte Cloud Monitoring dans `$SEC_PROJECT_ID`

Créez la métrique basée sur les logs **`iam-policy-change-count`** comptant chaque appel d'API contenant `SetIamPolicy`, puis créez une politique d'alerte **Cloud Monitoring** active surveillant `logging.googleapis.com/user/iam-policy-change-count` :

```bash
gcloud logging metrics create iam-policy-change-count \
  --project="${SEC_PROJECT_ID}" \
  --description="Count of SetIamPolicy mutations across the student environment" \
  --log-filter='protoPayload.methodName:"SetIamPolicy"'

TMP_ALERT="$(mktemp -d)"
cat <<'EOF' > "${TMP_ALERT}/alert-policy.json"
{
  "displayName": "Alert — IAM Policy Change Detected",
  "combiner": "OR",
  "enabled": true,
  "conditions": [
    {
      "displayName": "IAM SetIamPolicy Mutation Threshold",
      "conditionThreshold": {
        "filter": "metric.type=\"logging.googleapis.com/user/iam-policy-change-count\" AND resource.type=\"global\"",
        "comparison": "COMPARISON_GT",
        "thresholdValue": 0,
        "duration": "0s",
        "aggregations": [
          {
            "alignmentPeriod": "60s",
            "perSeriesAligner": "ALIGN_RATE"
          }
        ]
      }
    }
  ]
}
EOF

gcloud alpha monitoring policies create \
  --project="${SEC_PROJECT_ID}" \
  --policy-from-file="${TMP_ALERT}/alert-policy.json"
rm -rf "${TMP_ALERT}"
```

---

## Étape 6 : Vérification Finale du Module 6 (`100 / 100`) & Nettoyage de Fin de Journée 3 (`cleanup --day=3`)

1. Lancez la vérification officielle du Module 6 :

```bash
./bin/lab-ctl verify --day=3 --module=6
```

Assurez-vous d'obtenir **`Total Score: 100 / 100 points (All Passed: true)`**.

2. **Nettoyage FinOps de fin de Journée 3 (`cleanup --day=3`)** :
Maintenant que vos **deux modules de la Journée 3 (Modules 5 et 6)** sont validés à `100 / 100 points` et enregistrés sur votre Dashboard, exécutez la commande de nettoyage de fin de journée afin de rétablir l'ingress de `frontend` à `--ingress=all` et de supprimer les ressources du Load Balancer global facturées à l'heure entre le Jour 3 et le Jour 4 :

```bash
./bin/lab-ctl cleanup --day=3
```
