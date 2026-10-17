# Jour 1 — Module 1 : Fondations GCP, Politiques d'Organisation & Déploiement Multi-Projets

* **Durée estimée** : 1h30
* **Région cible** : `europe-west1` (Zone : `europe-west1-b`)
* **Projets concernés** : `$NET_PROJECT_ID`, `$FRONT_PROJECT_ID`, `$BACK_PROJECT_ID`, `$SEC_PROJECT_ID`

---

## 1. Objectifs Pédagogiques

Dans ce premier module, vous prenez en main votre **Landing Zone d'entreprise à 4 projets** (`folders/$FOLDER_ID`) sous l'organisation `gcp.telecom-paris.fr` et déployez la version initiale (v0) de l'application fil rouge **TelP Secure Shop** :

1. Découvrir l'isolation multi-projets (`net`, `front`, `back`, `sec`), vérifier l'association de votre compte de facturation et appliquer le principe de **moindre activation des API Cloud** (uniquement les API requises pour le Jour 1).
2. Observer en pratique les **10 Politiques d'Organisation (Organization Policies)** actives sur le dossier `Students` :
   * `constraints/gcp.resourceLocations` (`in:europe-west1-locations`) : souveraineté et résidence des données en Europe.
   * `constraints/iam.allowedPolicyMemberDomains` & `constraints/storage.publicAccessPrevention` : blocage de toute exposition anonyme (`allUsers` / `allAuthenticatedUsers`).
   * `constraints/compute.vmExternalIpAccess` (`denyAll: true`) & `constraints/compute.requireShieldedVm` : interdiction des adresses IP publiques sur les machines virtuelles et obligation des Shielded VMs.
3. Provisionner la base **Firestore Native `(default)`** et le bucket GCS **`gs://$BACK_PROJECT_ID-receipts`** dans `$BACK_PROJECT_ID`.
4. Déployer les microservices Cloud Run **`backend`** (dans `$BACK_PROJECT_ID`) et **`frontend`** (dans `$FRONT_PROJECT_ID`) et les tester via le CLI et **Cloud Shell Web Preview**.
5. Déployer la VM de diagnostic **`bastion`** (`e2-micro`, sans IP publique `--no-address`) dans `$BACK_PROJECT_ID`.
6. Maîtriser le cycle de vie complet de l'outil `./bin/lab-ctl` (`env`, `setup`, `verify`, `reset`, `cleanup`) dont chaque exécution est signée cryptographiquement (Ed25519) et remontée sur le tableau de bord de progression.

---

## 2. Architecture Cible du Module 1

```
+-----------------------------------------------------------------------------------+
| Dossier Étudiant (folders/$FOLDER_ID — Students/student-<slug>)                   |
| Org Policies: allowedPolicyMemberDomains | publicAccessPrevention | vmExternalIp  |
|                                                                                   |
|  +----------------------------------+       +----------------------------------+  |
|  | Front / DMZ ($FRONT_PROJECT_ID)  |       | Back / Data ($BACK_PROJECT_ID)   |  |
|  | APIs Jour 1 : run, iam           |       | APIs Jour 1 : run, compute,      |  |
|  |                                  |       |   firestore, storage, iam,       |  |
|  |  +----------------------------+  | HTTPS |   oslogin, logging, monitoring   |  |
|  |  | Cloud Run: frontend        |===========>  +----------------------------+ |  |
|  |  | (--no-allow-unauthenticated|  |       |   | Cloud Run: backend         | |  |
|  |  |  USE_OIDC_AUTH=false)      |  |       |   | (--no-allow-unauthenticated| |  |
|  |  +----------------------------+  |       |   +-------------+--------------+ |  |
|  +----------------------------------+       |                 |                |  |
|                                             |       +---------+---------+      |  |
|  +----------------------------------+       |       |                   |      |  |
|  | Réseau Host ($NET_PROJECT_ID)    |       |       v                   v      |  |
|  |  Facturation liée (0 API Jour 1) |       |  +----------+     +-----------+  |  |
|  +----------------------------------+       |  |Firestore |     | Bucket GCS|  |  |
|                                             |  |(default) |     | -receipts |  |  |
|  +----------------------------------+       |  +----------+     +-----------+  |  |
|  | SecOps ($SEC_PROJECT_ID)         |       |                                  |  |
|  |  Facturation liée (0 API Jour 1) |       |  +----------------------------+  |  |
|  +----------------------------------+       |  | VM GCE: bastion (e2-micro) |  |  |
|                                             |  | Zone: europe-west1-b       |  |  |
|                                             |  | IP Externe: AUCUNE         |  |  |
|                                             |  +----------------------------+  |  |
|                                             +----------------------------------+  |
+-----------------------------------------------------------------------------------+
```

---

## 3. Étape 1 — Initialisation de l'Environnement (`env`, `setup`) & Premier Diagnostic (`verify`)

1. Authentifiez vos Application Default Credentials (ADC) avec votre compte `@gcp.telecom-paris.fr` :

   ```bash
   gcloud auth application-default login --no-launch-browser
   ```

2. Découvrez vos 4 projets, liez automatiquement votre compte de facturation et exportez vos variables d'environnement :

   ```bash
   source <(./bin/lab-ctl env)
   echo "Dossier : folders/${FOLDER_ID} | Politique ACM : ${ACCESS_POLICY_ID}"
   echo "Projets : NET=${NET_PROJECT_ID} | FRONT=${FRONT_PROJECT_ID} | BACK=${BACK_PROJECT_ID} | SEC=${SEC_PROJECT_ID}"
   ```

3. Initialisez l'état de départ du Module 1 et lancez une première vérification à blanc :

   ```bash
   ./bin/lab-ctl setup --day=1 --module=1
   ./bin/lab-ctl verify --day=1 --module=1
   ```

   > **Observation** : `lab-ctl verify` retourne un score initial de **`0 / 100`** (`[FAIL]` sur `M1-01` à `M1-04`) avec des indices de remédiation, et met immédiatement à jour votre tableau de bord personnel (isolé pour votre compte) :
   > - **Page Principale (Résultat du dernier test/commande)** : `$STUDENT_DASHBOARD_URL` (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/<votre-email>/index.html`)
   > - **Page Logs History (Historique complet de vos journaux)** : `$STUDENT_LOGS_HISTORY_URL` (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/<votre-email>/history.html`)

---

## 4. Étape 2 — Activation Stricte des API du Jour 1 (`M1-01`)

Pour réduire la surface d'attaque du plan de contrôle GCP, seuls les services requis pour les travaux de la journée doivent être activés.

1. Activez les API requises pour le Jour 1 sur **`$FRONT_PROJECT_ID`** (`run`, `iam`) et sur **`$BACK_PROJECT_ID`** (`run`, `compute`, `firestore`, `storage`, `iam`, `oslogin`, `logging`, `monitoring`) :

   ```bash
   gcloud services enable run.googleapis.com iam.googleapis.com \
     --project="${FRONT_PROJECT_ID}"

   gcloud services enable \
     run.googleapis.com \
     compute.googleapis.com \
     firestore.googleapis.com \
     storage.googleapis.com \
     iam.googleapis.com \
     oslogin.googleapis.com \
     logging.googleapis.com \
     monitoring.googleapis.com \
     --project="${BACK_PROJECT_ID}"
   ```

   > **Important** : Ne **pas** activer d'API de charge de travail sur `$NET_PROJECT_ID` ou `$SEC_PROJECT_ID` au Jour 1, ni d'API des Jours 2 à 4 (`dns`, `iap`, `secretmanager`, `cloudkms`, `securitycenter`, `dlp`, `modelarmor`, `aiplatform`). Si vous activez prématurément une API d'un jour ultérieur, le contrôle `M1-01` la détectera et échouera.

---

## 5. Étape 3 — Provisionnement de Firestore & Cloud Storage (`M1-02`) et Test des Politiques d'Organisation

1. Créez la base de données **Firestore Native `(default)`** dans `europe-west1` sur `$BACK_PROJECT_ID` :

   ```bash
   gcloud firestore databases create \
     --database="(default)" \
     --location="${REGION}" \
     --type=firestore-native \
     --project="${BACK_PROJECT_ID}"
   ```

2. **Test de cas limite (Souveraineté `constraints/gcp.resourceLocations`)** — Tentez de créer un bucket aux États-Unis (`us-central1`) :

   ```bash
   gcloud storage buckets create "gs://${BACK_PROJECT_ID}-us-test" \
     --project="${BACK_PROJECT_ID}" \
     --location="us-central1"
   ```

   > **Résultat attendu (`HTTPError 412`)** : La politique d'organisation `constraints/gcp.resourceLocations` bloque immédiatement la création hors de `europe-west1`.

3. Créez le bucket des reçus **`gs://${BACK_PROJECT_ID}-receipts`** dans `europe-west1` (sans Uniform Bucket-Level Access pour l'état initial du Module 1, que vous durcirez au Module 2) :

   ```bash
   gcloud storage buckets create "gs://${BACK_PROJECT_ID}-receipts" \
     --project="${BACK_PROJECT_ID}" \
     --location="${REGION}" \
     --no-uniform-bucket-level-access
   ```

4. **Test de cas limite (`constraints/iam.allowedPolicyMemberDomains`)** — Tentez d'accorder l'accès à un domaine externe non autorisé (`domain:google.com`) :

   ```bash
   gcloud storage buckets add-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="domain:google.com" \
     --role="roles/storage.objectViewer" \
     --project="${BACK_PROJECT_ID}"
   ```

   > **Résultat attendu (`HTTPError 412: One or more users named in the policy do not belong to a permitted customer`)** : L'organisation refuse tout partage hors du domaine `gcp.telecom-paris.fr` (`constraints/iam.allowedPolicyMemberDomains` et `constraints/storage.publicAccessPrevention`). Accordez à la place l'accès au domaine interne `domain:gcp.telecom-paris.fr` (baseline v0 que vous restreindrez à `sa-backend` au Module 2) :

   ```bash
   gcloud storage buckets add-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="domain:gcp.telecom-paris.fr" \
     --role="roles/storage.objectViewer" \
     --project="${BACK_PROJECT_ID}"
   ```

5. **Vérification intermédiaire (Retrial `50 / 100`)** — Lancez `lab-ctl verify` pour constater que `M1-01` et `M1-02` passent (`50/100`) tandis que `M1-03` et `M1-04` restent à compléter :

   ```bash
   ./bin/lab-ctl verify --day=1 --module=1
   ```

---

## 6. Étape 4 — Déploiement Cloud Run (`backend` & `frontend`) (`M1-03`)

Les images pré-compilées de **TelP Secure Shop** sont hébergées dans le registre central `europe-west1-docker.pkg.dev/telp-admin-central/telp-shop`.

1. Dans `$BACK_PROJECT_ID`, assurez-vous que le compte de service Compute par défaut dispose du rôle `roles/editor` pour la baseline du Module 1 (vous supprimerez ce rôle trop permissif au Module 2), puis déployez **`backend`** :

   ```bash
   gcloud projects add-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:${BACK_PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
     --role="roles/editor"

   gcloud run deploy backend \
     --image="europe-west1-docker.pkg.dev/telp-admin-central/telp-shop/backend:latest" \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --no-allow-unauthenticated \
     --min-instances=0 \
     --max-instances=3 \
     --set-env-vars="GCP_PROJECT_ID=${BACK_PROJECT_ID},RECEIPTS_BUCKET=${BACK_PROJECT_ID}-receipts,FIRESTORE_DB=(default),FIRESTORE_COLLECTION=orders,RECEIPT_SIGNING_SECRET=INSECURE_DEFAULT_SECRET_2026"

   gcloud run services add-iam-policy-binding backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --member="domain:gcp.telecom-paris.fr" \
     --role="roles/run.invoker"

   gcloud run services add-iam-policy-binding backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --member="serviceAccount:${FRONT_PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
     --role="roles/run.invoker"

   export BACKEND_URL="$(gcloud run services describe backend --project="${BACK_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   echo "BACKEND_URL=${BACKEND_URL}"
   ```

2. Dans `$FRONT_PROJECT_ID`, déployez **`frontend`** en pointant `BACKEND_URL` vers le service `backend` :

   ```bash
   gcloud run deploy frontend \
     --image="europe-west1-docker.pkg.dev/telp-admin-central/telp-shop/frontend:latest" \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --no-allow-unauthenticated \
     --min-instances=0 \
     --max-instances=3 \
     --set-env-vars="GCP_PROJECT_ID=${FRONT_PROJECT_ID},BACKEND_URL=${BACKEND_URL},USE_OIDC_AUTH=false"

   gcloud run services add-iam-policy-binding frontend \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --member="domain:gcp.telecom-paris.fr" \
     --role="roles/run.invoker"

   export FRONTEND_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   echo "FRONTEND_URL=${FRONTEND_URL}"
   ```

3. Testez l'application **TelP Secure Shop** (`frontend` et `backend`) :
   * **Option A — Interface Graphique (Navigateur / Cloud Shell Web Preview)** :
     Comme `constraints/iam.allowedPolicyMemberDomains` interdit `allUsers`, l'accès direct non authentifié à l'URL `.a.run.app` dans un navigateur renvoie `403 Forbidden`. Utilisez le proxy d'authentification Cloud Run qui injecte automatiquement votre jeton OIDC `@gcp.telecom-paris.fr` :
     ```bash
     gcloud run services proxy frontend \
       --project="${FRONT_PROJECT_ID}" \
       --region="${REGION}" \
       --port=8080
     ```
     Ouvrez ensuite **Web Preview -> Preview on port 8080** dans Google Cloud Shell (ou `http://localhost:8080` sur un poste local) pour afficher la boutique, passer une commande (`POST /checkout`), vérifier le reçu signé HMAC (`GET /receipts/{id}`) et consulter le portail `/admin`.
   * **Option B — Ligne de Commande (`curl` + jeton OIDC)** :
     ```bash
     TOKEN="${LAB_CTL_IDENTITY_TOKEN:-$(gcloud auth print-identity-token)}"
     curl -sS -H "Authorization: Bearer ${TOKEN}" "${BACKEND_URL}/health"
     curl -sS -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/health"
     curl -sS -X POST -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
       -d '{"customer_name":"Etudiant Test","customer_email":"etudiant.test@gcp.telecom-paris.fr","amount_eur":49.90,"items":["yubikey-5c"]}' \
       "${FRONTEND_URL}/checkout"
     ```

---

## 7. Étape 5 — Création du Réseau Bootstrap & de la VM Interne `bastion` (`M1-04`)

La politique d'organisation `constraints/compute.skipDefaultNetworkCreation` empêche la création du réseau `default` global, et `constraints/compute.vmExternalIpAccess` interdit toute adresse IP publique sur les VMs.

1. Créez un réseau VPC temporaire `vpc-bootstrap` et un sous-réseau `subnet-bootstrap` (`10.0.1.0/24`) dans `$BACK_PROJECT_ID` :

   ```bash
   gcloud compute networks create vpc-bootstrap \
     --project="${BACK_PROJECT_ID}" \
     --subnet-mode=custom

   gcloud compute networks subnets create subnet-bootstrap \
     --project="${BACK_PROJECT_ID}" \
     --network=vpc-bootstrap \
     --region="${REGION}" \
     --range="10.0.1.0/24"
   ```

2. **Test de cas limite (`constraints/compute.vmExternalIpAccess`)** — Tentez de créer `bastion` sans l'option `--no-address` (avec une IP externe par défaut) :

   ```bash
   gcloud compute instances create bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="europe-west1-b" \
     --machine-type=e2-micro \
     --network=vpc-bootstrap \
     --subnet=subnet-bootstrap \
     --image-family=debian-12 \
     --image-project=debian-cloud \
     --shielded-secure-boot \
     --shielded-vtpm \
     --shielded-integrity-monitoring
   ```

   > **Résultat attendu (`Constraint constraints/compute.vmExternalIpAccess violated`)** : L'organisation bloque l'attachement d'une IP publique externe.

3. **Test de cas limite (`constraints/compute.requireShieldedVm`)** — Tentez de créer `bastion` sans vTPM (`--no-shielded-vtpm`) :

   ```bash
   gcloud compute instances create bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="europe-west1-b" \
     --machine-type=e2-micro \
     --network=vpc-bootstrap \
     --subnet=subnet-bootstrap \
     --no-address \
     --image-family=debian-12 \
     --image-project=debian-cloud \
     --no-shielded-vtpm
   ```

   > **Résultat attendu (`Constraint constraints/compute.requireShieldedVm violated`)** : L'organisation impose le démarrage sécurisé et le module vTPM sur toutes les VMs.

4. Créez maintenant la VM Shielded `bastion` en spécifiant **`--no-address`** :

   ```bash
   gcloud compute instances create bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="europe-west1-b" \
     --machine-type=e2-micro \
     --network=vpc-bootstrap \
     --subnet=subnet-bootstrap \
     --no-address \
     --image-family=debian-12 \
     --image-project=debian-cloud \
     --shielded-secure-boot \
     --shielded-vtpm \
     --shielded-integrity-monitoring
   ```

---

## 8. Étape 6 — Validation Finale (`100 / 100`) & Commandes de Gestion du Cycle de Vie (`cleanup`, `reset`)

1. Validez l'ensemble des 4 contrôles du Module 1 (`M1-01` à `M1-04`) :

   ```bash
   ./bin/lab-ctl verify --day=1 --module=1
   ```

   Vous devez obtenir **`Total Score: 100 / 100 points (All Passed: true)`**.

2. **Gestion des coûts et du cycle de vie (`cleanup` et `reset`)** :
   * **En fin de journée (`cleanup`)** : Pour arrêter la VM `bastion` et éviter toute consommation horaire inutile entre deux séances :
     ```bash
     ./bin/lab-ctl cleanup --day=1
     ```
   * **Réinitialisation complète d'un module (`reset`)** : Si vous souhaitez réinitialiser les ressources et API du Module 1 à leur état pré-module pour recommencer l'exercice :
     ```bash
     ./bin/lab-ctl reset --day=1 --module=1
     ```
