# Jour 1 — Module 2 : IAM Moindre Privilège, Comptes de Service Dédiés & OIDC Inter-Projets

* **Durée estimée** : 2h00
* **Région cible** : `europe-west1` (Zone : `europe-west1-b`)
* **Projets utilisés pour les charges de travail** : `$FRONT_PROJECT_ID` (`telp-<account-id>-front`), `$BACK_PROJECT_ID` (`telp-<account-id>-back`)
* **Projets non utilisés pour les charges de travail à cette étape (aucune API de charge de travail activée)** :
  * `$NET_PROJECT_ID` (`telp-<account-id>-net`) : réservé au réseau partagé (**Shared VPC Host**), qui ne sera mis en œuvre qu'au **Jour 2 (Module 3)**.
  * `$SEC_PROJECT_ID` (`telp-<account-id>-sec`) : réservé au chiffrement centralisé **Cloud KMS (CMEK)** et aux journaux d'audit immuables (**WORM**), qui ne seront mis en œuvre qu'au **Jour 3 (Module 6)**.
  * *(Au Module 2, seule l'API de plan de contrôle `iam.googleapis.com` pré-activée est interrogée sur l'ensemble des 4 projets lors du contrôle `M2-04` afin de vérifier l'absence totale de clés de comptes de service gérées par l'utilisateur ; aucune API de charge de travail ne doit être activée sur `$NET_PROJECT_ID` ni sur `$SEC_PROJECT_ID`).*

---

## 1. Objectifs Pédagogiques & Différences avec le Module 1

Au **Module 1**, l'objectif était de mettre en place les fondations de votre Landing Zone et de déployer une première version fonctionnelle (**v0**) de l'application **TelP Secure Shop**. Pour accélérer ce premier déploiement, plusieurs raccourcis IAM volontairement permissifs ont été utilisés :
* `frontend`, `backend` et la VM `bastion` s'exécutaient avec le **compte de service Compute Engine par défaut** (`<project-number>-compute@developer.gserviceaccount.com`) doté du rôle primitif **`roles/editor`** sur l'ensemble du projet.
* Le service interne `backend` autorisait tout le domaine `domain:gcp.telecom-paris.fr` en tant que `roles/run.invoker`, et `frontend` appelait `backend` avec `USE_OIDC_AUTH=false`.
* Le bucket `gs://$BACK_PROJECT_ID-receipts` avait **Uniform Bucket-Level Access (UBLA)** désactivé et accordait `roles/storage.objectViewer` à tout le domaine `domain:gcp.telecom-paris.fr`.

Dans ce **Module 2**, vous ne redéployez pas de nouvelle infrastructure : vous **durcissez l'architecture des identités (IAM Zero-Trust)** de l'application existante :
1. **Comptes de service dédiés & Suppression du rôle `Editor` par défaut (`M2-01`)** : Créer 3 comptes de service dédiés (`sa-frontend`, `sa-backend`, `sa-bastion`), les attacher aux charges de travail et révoquer `roles/editor` sur les comptes de service Compute Engine par défaut de `$FRONT_PROJECT_ID` et `$BACK_PROJECT_ID`.
2. **Authentification OIDC inter-projets de service à service (`M2-02`)** : Configurer `frontend` (`USE_OIDC_AUTH=true`) pour générer un jeton d'identité OIDC signé par Google via `sa-frontend`, et restreindre `roles/run.invoker` sur `backend` **exclusivement** à `serviceAccount:sa-frontend@$FRONT_PROJECT_ID.iam.gserviceaccount.com` (en révoquant `domain:gcp.telecom-paris.fr` et le compte Compute par défaut).
3. **Uniform Bucket-Level Access (UBLA) & Moindre Privilège sur les données (`M2-03`)** : Activer UBLA sur `gs://$BACK_PROJECT_ID-receipts`, révoquer l'accès de domaine sur le bucket, et accorder uniquement `roles/storage.objectAdmin` (au niveau du bucket) + `roles/datastore.user` (au niveau du projet `$BACK_PROJECT_ID`) à `sa-backend`, ainsi que `roles/logging.logWriter` et `roles/monitoring.metricWriter` à `sa-bastion`.
4. **Zéro clé statique (`USER_MANAGED`) & Activation d'OS Login (`M2-04`)** : Auditer et supprimer toute clé JSON statique de compte de service sur vos 4 projets, et activer **OS Login** (`enable-oslogin=TRUE`) dans les métadonnées Compute du projet `$BACK_PROJECT_ID`.

---

## 2. Architecture Cible du Module 2

```
+-----------------------------------------------------------------------------------+
| Dossier Étudiant (folders/$FOLDER_ID — Students/student-<account-id>)             |
|                                                                                   |
|  +----------------------------------+       +----------------------------------+  |
|  | Front / DMZ ($FRONT_PROJECT_ID)  |       | Back / Data ($BACK_PROJECT_ID)   |  |
|  | Default Compute SA : 0 Editor    |       | Default Compute SA : 0 Editor    |  |
|  |                                  |       | Metadata : enable-oslogin=TRUE   |  |
|  |  +----------------------------+  | OIDC  |   +----------------------------+ |  |
|  |  | Cloud Run: frontend        |===========>  | Cloud Run: backend         | |  |
|  |  | SA: sa-frontend            |  | Token |   | SA: sa-backend             | |  |
|  |  | USE_OIDC_AUTH=true         |  |       |   | Invoker: UNIQUEMENT        | |  |
|  |  | Invoker: domain:telecom... |  |       |   |   sa-frontend@front        | |  |
|  |  +----------------------------+  |       |   +-------------+--------------+ |  |
|  +----------------------------------+       |                 |                |  |
|                                             |       +---------+---------+      |  |
|  +----------------------------------+       |       |                   |      |  |
|  | Réseau Host ($NET_PROJECT_ID)    |       |       v (datastore.user)  v      |  |
|  |  Non utilisé au Jour 1           |       |  +----------+     +-----------+  |  |
|  |  (0 API de charge de travail,    |       |  |Firestore |     | Bucket GCS|  |  |
|  |   0 clé SA USER_MANAGED)         |       |  |(default) |     | -receipts |  |  |
|  +----------------------------------+       |  +----------+     | UBLA=true |  |  |
|                                             |                   | (object   |  |  |
|  +----------------------------------+       |                   |  Admin)   |  |  |
|  | SecOps ($SEC_PROJECT_ID)         |       |                   +-----------+  |  |
|  |  Non utilisé au Jour 1           |       |  +----------------------------+  |  |
|  |  (0 API de charge de travail,    |       |  | VM GCE: bastion (e2-micro) |  |  |
|  |   0 clé SA USER_MANAGED)         |       |  | SA: sa-bastion             |  |  |
|  +----------------------------------+       |  | (logWriter + metricWriter) |  |  |
|                                             +----------------------------------+  |
+-----------------------------------------------------------------------------------+
```

---

## 3. Étape 1 — Initialisation du Module 2 (`env`, `setup`) & Diagnostic Initial (`verify`)

1. Depuis **Google Cloud Shell**, chargez vos variables d'environnement si ce n'est pas déjà fait :

   ```bash
   source <(./bin/lab-ctl env)
   echo "Compte  : ${STUDENT_EMAIL}"
   echo "Projets : FRONT=${FRONT_PROJECT_ID} | BACK=${BACK_PROJECT_ID} | NET=${NET_PROJECT_ID} | SEC=${SEC_PROJECT_ID}"
   ```

2. Préparez l'état initial du Module 2 (qui injecte notamment une clé statique historique `USER_MANAGED` à auditer dans `$BACK_PROJECT_ID`) et lancez une première vérification :

   ```bash
   ./bin/lab-ctl setup --day=1 --module=2
   ./bin/lab-ctl verify --day=1 --module=2
   ```

   > **Observation** : Les 4 contrôles (`M2-01` à `M2-04`) affichent `[FAIL]` (`0 / 100 points`), en indiquant précisément les écarts IAM détectés sur vos ressources du Module 1 ainsi que la présence d'une clé `USER_MANAGED`.

---

## 4. Étape 2 — Création des Comptes de Service Dédiés & Moindre Privilège sur les Données (`M2-01` & `M2-03`)

1. Créez les 3 comptes de service dédiés dans leurs projets respectifs :

   ```bash
   # Compte de service du microservice frontend (dans $FRONT_PROJECT_ID)
   gcloud iam service-accounts create sa-frontend \
     --display-name="TelP Shop Frontend Runtime SA" \
     --project="${FRONT_PROJECT_ID}"

   # Compte de service du microservice backend (dans $BACK_PROJECT_ID)
   gcloud iam service-accounts create sa-backend \
     --display-name="TelP Shop Backend Runtime SA" \
     --project="${BACK_PROJECT_ID}"

   # Compte de service de la VM bastion (dans $BACK_PROJECT_ID)
   gcloud iam service-accounts create sa-bastion \
     --display-name="TelP Shop Bastion VM SA" \
     --project="${BACK_PROJECT_ID}"

   export SA_FRONT="sa-frontend@${FRONT_PROJECT_ID}.iam.gserviceaccount.com"
   export SA_BACK="sa-backend@${BACK_PROJECT_ID}.iam.gserviceaccount.com"
   export SA_BASTION="sa-bastion@${BACK_PROJECT_ID}.iam.gserviceaccount.com"
   ```

2. Activez **Uniform Bucket-Level Access (UBLA)** sur le bucket des reçus `gs://${BACK_PROJECT_ID}-receipts`, révoquez le droit de lecture global du domaine `gcp.telecom-paris.fr` posé au Module 1, et accordez `roles/storage.objectAdmin` **uniquement sur ce bucket** à `sa-backend` :

   ```bash
   gcloud storage buckets update "gs://${BACK_PROJECT_ID}-receipts" \
     --uniform-bucket-level-access \
     --project="${BACK_PROJECT_ID}"

   gcloud storage buckets remove-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="domain:gcp.telecom-paris.fr" \
     --role="roles/storage.objectViewer" \
     --project="${BACK_PROJECT_ID}"

   gcloud storage buckets add-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="serviceAccount:${SA_BACK}" \
     --role="roles/storage.objectAdmin" \
     --project="${BACK_PROJECT_ID}"
   ```

   > **Attention (Moindre privilège sur Cloud Storage)** : N'accordez **pas** `roles/storage.admin` ni `roles/storage.objectAdmin` au niveau du projet `$BACK_PROJECT_ID`. Le droit `roles/storage.objectAdmin` doit être borné à la ressource `gs://${BACK_PROJECT_ID}-receipts`.

3. Accordez à `sa-backend` le rôle `roles/datastore.user` sur `$BACK_PROJECT_ID` (pour lire et écrire les commandes dans Firestore Native), et accordez à `sa-bastion` uniquement les rôles de télémétrie (`roles/logging.logWriter` et `roles/monitoring.metricWriter`) :

   ```bash
   gcloud projects add-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:${SA_BACK}" \
     --role="roles/datastore.user"

   gcloud projects add-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:${SA_BASTION}" \
     --role="roles/logging.logWriter"

   gcloud projects add-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:${SA_BASTION}" \
     --role="roles/monitoring.metricWriter"
   ```

4. Attachez `sa-bastion` à la VM `bastion` (ce qui nécessite d'arrêter puis de redémarrer l'instance) :

   ```bash
   gcloud compute instances stop bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}"

   gcloud compute instances set-service-account bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --service-account="${SA_BASTION}" \
     --scopes="https://www.googleapis.com/auth/cloud-platform"

   gcloud compute instances start bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}"
   ```

---

## 5. Étape 3 — Authentification OIDC Inter-Projets sur Cloud Run & Révocation des Rôles `Editor` (`M2-01` & `M2-02`)

1. Mettez à jour le service Cloud Run **`backend`** pour qu'il s'exécute sous l'identité `sa-backend` :

   ```bash
   gcloud run services update backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --service-account="${SA_BACK}"
   ```

2. Restreignez le rôle `roles/run.invoker` sur **`backend`** **exclusivement** au compte de service `sa-frontend` du projet `$FRONT_PROJECT_ID`, et supprimez les liaisons permissives du Module 1 (`domain:gcp.telecom-paris.fr` et le compte de service Compute par défaut) :

   ```bash
   gcloud run services add-iam-policy-binding backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --member="serviceAccount:${SA_FRONT}" \
     --role="roles/run.invoker"

   gcloud run services remove-iam-policy-binding backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --member="domain:gcp.telecom-paris.fr" \
     --role="roles/run.invoker"

   gcloud run services remove-iam-policy-binding backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --member="serviceAccount:${FRONT_PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
     --role="roles/run.invoker"
   ```

3. Mettez à jour le service Cloud Run **`frontend`** pour qu'il s'exécute sous l'identité `sa-frontend` et activez l'authentification OIDC inter-services (`USE_OIDC_AUTH=true`) :

   ```bash
   gcloud run services update frontend \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --service-account="${SA_FRONT}" \
     --update-env-vars="USE_OIDC_AUTH=true"
   ```

4. Maintenant que `frontend`, `backend` et `bastion` utilisent leurs comptes de service dédiés, révoquez le rôle primitif **`roles/editor`** des comptes de service Compute Engine par défaut sur `$FRONT_PROJECT_ID` et `$BACK_PROJECT_ID` :

   ```bash
   gcloud projects remove-iam-policy-binding "${FRONT_PROJECT_ID}" \
     --member="serviceAccount:${FRONT_PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
     --role="roles/editor"

   gcloud projects remove-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:${BACK_PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
     --role="roles/editor"
   ```

   > **Vérification intermédiaire** : Exécutez `./bin/lab-ctl verify --day=1 --module=2`. Les contrôles **`M2-01`**, **`M2-02`** et **`M2-03`** doivent désormais passer à **`[PASS]`** (`75 / 100 points`).

---

## 6. Étape 4 — Audit et Suppression des Clés Statiques (`USER_MANAGED`) & Activation d'OS Login (`M2-04`)

Les clés JSON exportées (`USER_MANAGED`) constituent l'un des principaux vecteurs de compromission sur le Cloud. Toutes les charges de travail GCP doivent utiliser des jetons éphémères fournis par le serveur de métadonnées, et l'accès SSH aux VMs doit passer par **OS Login** (lié à l'identité IAM) plutôt que par des clés SSH statiques dans les métadonnées.

1. Auditez l'ensemble des comptes de service sur vos **4 projets** (`$NET_PROJECT_ID`, `$FRONT_PROJECT_ID`, `$BACK_PROJECT_ID`, `$SEC_PROJECT_ID`) pour identifier toute clé `USER_MANAGED` :

   ```bash
   for proj in "${NET_PROJECT_ID}" "${FRONT_PROJECT_ID}" "${BACK_PROJECT_ID}" "${SEC_PROJECT_ID}"; do
     echo "=== Projet : ${proj} ==="
     for sa in $(gcloud iam service-accounts list --project="${proj}" --format="value(email)"); do
       gcloud iam service-accounts keys list \
         --iam-account="${sa}" \
         --project="${proj}" \
         --managed-by=user \
         --format="table(name.basename():label=KEY_ID, validAfterTime, validBeforeTime)"
     done
   done
   ```

2. Supprimez toutes les clés `USER_MANAGED` identifiées (notamment celle présente sur `sa-legacy-export@${BACK_PROJECT_ID}.iam.gserviceaccount.com`) :

   ```bash
   for proj in "${NET_PROJECT_ID}" "${FRONT_PROJECT_ID}" "${BACK_PROJECT_ID}" "${SEC_PROJECT_ID}"; do
     for sa in $(gcloud iam service-accounts list --project="${proj}" --format="value(email)"); do
       for key_id in $(gcloud iam service-accounts keys list --iam-account="${sa}" --project="${proj}" --managed-by=user --format="value(name.basename())"); do
         echo "Suppression de la clé USER_MANAGED ${key_id} sur ${sa} (${proj})..."
         gcloud iam service-accounts keys delete "${key_id}" \
           --iam-account="${sa}" \
           --project="${proj}" \
           --quiet
       done
     done
   done
   ```

3. Activez **OS Login** à l'échelle du projet `$BACK_PROJECT_ID` :

   ```bash
   gcloud compute project-info add-metadata \
     --project="${BACK_PROJECT_ID}" \
     --metadata="enable-oslogin=TRUE"
   ```

---

## 7. Étape 5 — Vérification de l'Accès à l'Application & Preuve d'Isolation OIDC

1. Récupérez les URLs de vos deux services Cloud Run :

   ```bash
   export BACKEND_URL="$(gcloud run services describe backend --project="${BACK_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   export FRONTEND_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   echo "FRONTEND_URL = ${FRONTEND_URL}"
   echo "BACKEND_URL  = ${BACKEND_URL}"
   ```

2. **Test d'isolation du `backend` (preuve que l'accès direct utilisateur est désormais bloqué)** — Tentez d'appeler directement `BACKEND_URL/health` avec votre propre jeton d'identité `@gcp.telecom-paris.fr` :

   ```bash
   curl -i -s \
     -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
     "${BACKEND_URL}/health" | head -n 12
   ```

   > **Résultat attendu (`HTTP/2 403 Forbidden`)** : Contrairement au Module 1, même un utilisateur authentifié du domaine `@gcp.telecom-paris.fr` ne peut plus invoquer directement le microservice interne `backend`. Seul le compte de service `sa-frontend@${FRONT_PROJECT_ID}.iam.gserviceaccount.com` possède `roles/run.invoker`.

3. **Test de bout en bout via `frontend` (avec jeton OIDC `sa-frontend` vers `backend`)** :

   ```bash
   # 1. Santé du frontend (et vérification de sa connexion OIDC vers le backend)
   curl -s -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
     "${FRONTEND_URL}/health" ; echo

   # 2. Création d'une commande (frontend -> OIDC token sa-frontend -> backend -> Firestore + GCS UBLA)
   ORDER_JSON="$(curl -s -X POST "${FRONTEND_URL}/checkout" \
     -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
     -H "Content-Type: application/json" \
     -d '{"customer_name":"Alice Telecom","customer_email":"alice@gcp.telecom-paris.fr","amount_eur":42.50,"items":["Zero-Trust IAM Guide"]}')"
   echo "${ORDER_JSON}"

   # 3. Lecture du reçu signé depuis le bucket GCS (avec UBLA activé) via sa-backend
   ORDER_ID="$(echo "${ORDER_JSON}" | sed -n 's/.*"order_id":"\([^"]*\)".*/\1/p')"
   curl -s -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
     "${FRONTEND_URL}/receipts/${ORDER_ID}" ; echo
   ```

4. **Accès visuel depuis votre navigateur (Cloud Shell Web Preview)** :

   ```bash
   gcloud run services proxy frontend \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --port=8080
   ```

   Cliquez ensuite sur l'icône **Web Preview** (Aperçu sur le Web) en haut à droite de Google Cloud Shell $\rightarrow$ **Preview on port 8080** pour visualiser l'interface graphique **TelP Secure Shop** et passer une commande de test, puis appuyez sur `Ctrl+C` pour arrêter le proxy.

---

## 8. Étape 6 — Validation Finale du Module 2 (`verify`) & Nettoyage de Fin de Journée (`cleanup`)

1. Lancez la vérification officielle du Module 2 :

   ```bash
   ./bin/lab-ctl verify --day=1 --module=2
   ```

   **Résultat attendu (`100 / 100 points`)** :
   ```text
   ======================================================================
     TelP Cloud Security Verification — Day 1 Module 2 (day1-mod2)
   ======================================================================
     [PASS] M2-01  (25/25 pts) Dedicated Service Accounts & No Default Editor
     [PASS] M2-02  (25/25 pts) Cross-Project Cloud Run OIDC Invoker Enforcement
     [PASS] M2-03  (25/25 pts) Bucket UBLA & Least-Privilege Data Access
     [PASS] M2-04  (25/25 pts) Zero User-Managed SA Keys & OS Login Active
   ----------------------------------------------------------------------
     Total Score: 100 / 100 points (All Passed: true)
   ======================================================================
   ```

2. Consultez votre tableau de bord personnel mis à jour :
   * **Page Principale (Dernier résultat)** : `echo "${STUDENT_DASHBOARD_URL}"`
   * **Page Logs History (Historique complet)** : `echo "${STUDENT_LOGS_HISTORY_URL}"`

3. **En fin de Journée 1**, arrêtez la VM `bastion` pour économiser votre budget Cloud (tout en conservant vos services Cloud Run serverless à l'échelle zéro, vos comptes de service, votre base Firestore et votre bucket GCS pour le Jour 2) :

   ```bash
   ./bin/lab-ctl cleanup --day=1
   ```
