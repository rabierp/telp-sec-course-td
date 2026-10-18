# Jour 1 — Module 1 : Fondations GCP, Politiques d'Organisation & Déploiement Multi-Projets

* **Durée estimée** : 1h30
* **Région cible** : `europe-west1` (Zone : `europe-west1-b`)
* **Projets utilisés pour les charges de travail au Jour 1** : `$FRONT_PROJECT_ID` (`telp-<account-id>-front`), `$BACK_PROJECT_ID` (`telp-<account-id>-back`)
* **Projets non utilisés à cette étape (aucune API de charge de travail activée)** :
  * `$NET_PROJECT_ID` (`telp-<account-id>-net`) : réservé au réseau partagé (**Shared VPC Host**), qui ne sera déployé qu'à partir du **Jour 2 (Module 3)**.
  * `$SEC_PROJECT_ID` (`telp-<account-id>-sec`) : réservé au chiffrement centralisé **Cloud KMS (CMEK)** et au stockage immuable des journaux d'audit (**WORM**), qui ne seront mis en œuvre qu'à partir du **Jour 3 (Module 6)**.
  * *(Au Jour 1, ces deux projets ont uniquement leur compte de facturation lié et l'API de plan de contrôle `iam.googleapis.com` pré-activée ; aucune API de charge de travail comme `compute`, `dns`, `cloudkms` ou `storage` ne doit y être activée).*

---

## 1. Objectifs Pédagogiques

Dans ce premier module, vous prenez en main votre **Landing Zone d'entreprise à 4 projets** (`folders/$FOLDER_ID`) sous l'organisation `gcp.telecom-paris.fr` et déployez la version initiale (v0) de l'application fil rouge **TelP Secure Shop** :

1. Découvrir l'isolation multi-projets (`net`, `front`, `back`, `sec`), vérifier l'association de votre compte de facturation et appliquer le principe de **moindre activation des API Cloud** (uniquement les API requises pour le Jour 1).
2. Observer en pratique les **10 Politiques d'Organisation (Organization Policies)** actives sur le dossier `Students` :
   * `constraints/gcp.resourceLocations` (`in:europe-west1-locations`) : résidence des données en Europe.
   * `constraints/iam.allowedPolicyMemberDomains` & `constraints/storage.publicAccessPrevention` : blocage de toute exposition anonyme (`allUsers` / `allAuthenticatedUsers`).
   * `constraints/compute.vmExternalIpAccess` (`denyAll: true`) & `constraints/compute.requireShieldedVm` : interdiction des adresses IP publiques sur les machines virtuelles et obligation des Shielded VMs.
3. Provisionner la base **Firestore Native `(default)`** et le bucket GCS **`gs://$BACK_PROJECT_ID-receipts`** dans `$BACK_PROJECT_ID`.
4. Déployer les microservices Cloud Run **`backend`** (dans `$BACK_PROJECT_ID`) et **`frontend`** (dans `$FRONT_PROJECT_ID`) et les tester via le CLI et **Cloud Shell Web Preview**.
5. Déployer la VM de diagnostic **`bastion`** (`e2-micro`, sans IP publique `--no-address`) dans `$BACK_PROJECT_ID`.
6. Maîtriser le cycle de vie complet de l'outil `./bin/lab-ctl` (`env`, `setup`, `verify`, `reset`, `cleanup`) dont chaque exécution est signée cryptographiquement (Ed25519) et remontée sur le tableau de bord de progression.

---

## 2. Architecture Fil Rouge (Landing Zone à 4 Projets) & Cible du Module 1

Dans votre dossier dédié `folders/$FOLDER_ID` (`Students/student-<account-id>`) sous l'organisation `gcp.telecom-paris.fr` (région `europe-west1`), vous disposez d'une **Landing Zone d'entreprise à 4 projets pré-créés** qui isole strictement les responsabilités réseau, DMZ, données internes et SecOps tout au long des 4 journées :

1. **`telp-<account-id>-net` (`$NET_PROJECT_ID`) — Projet Hôte Réseau (Shared VPC Host)** :
   Hébergera à partir du Jour 2 le réseau partagé `vpc-shared`, les sous-réseaux segmentés (`subnet-front`, `subnet-back`, `subnet-bastion`), Private Google Access, Cloud Router + Cloud NAT (`nat-gw`), Private Cloud DNS (`zone-internal`) et les règles de pare-feu. *(Au Jour 1, aucune API de charge de travail n'y est activée).*
2. **`telp-<account-id>-front` (`$FRONT_PROJECT_ID`) — Projet de Service DMZ (Exposition Publique)** :
   Héberge le microservice Cloud Run `frontend` (ainsi qu'au Jour 3 le Global External Application Load Balancer `url-map-frontend`, Cloud Armor WAF `armor-frontend-waf` et Identity-Aware Proxy sur `/admin`).
3. **`telp-<account-id>-back` (`$BACK_PROJECT_ID`) — Projet de Service Interne (Données & IA)** :
   Héberge le microservice Cloud Run `backend` (et `ai-assistant` au Jour 4), la VM de diagnostic `bastion` (`e2-micro` sans IP publique), la base Firestore Native `(default)`, le bucket GCS des reçus (`gs://$BACK_PROJECT_ID-receipts`), Secret Manager (`receipt-signing-secret`), Cloud DLP et Model Armor + Vertex AI.
4. **`telp-<account-id>-sec` (`$SEC_PROJECT_ID`) — Projet SecOps & Cryptographie Centrale** :
   Centralisera à partir du Jour 3 les clés Cloud KMS CMEK (`kr-central/key-app-storage`), le bucket verrouillé des journaux d'audit (`gs://$SEC_PROJECT_ID-audit-logs` alimenté par le puits agrégé `sink-folder-audit`) et les alertes Cloud Monitoring. *(Au Jour 1, aucune API de charge de travail n'y est activée).*

### Topologie Déployée au Module 1

```
+-----------------------------------------------------------------------------------+
| Dossier Étudiant (folders/$FOLDER_ID — Students/student-<account-id>)             |
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

1. Depuis **Google Cloud Shell** (où votre session est déjà authentifiée automatiquement avec votre compte `@gcp.telecom-paris.fr`), découvrez vos 4 projets, liez automatiquement votre compte de facturation et exportez vos variables d'environnement :

   ```bash
   source <(./bin/lab-ctl env)
   echo "Compte  : ${STUDENT_EMAIL}"
   echo "Dossier : folders/${FOLDER_ID} | Politique ACM : ${ACCESS_POLICY_ID}"
   echo "Projets : NET=${NET_PROJECT_ID} | FRONT=${FRONT_PROJECT_ID} | BACK=${BACK_PROJECT_ID} | SEC=${SEC_PROJECT_ID}"
   ```

   > **Note (Poste local hors Cloud Shell uniquement)** : Dans Google Cloud Shell, vous n'avez **pas** besoin d'exécuter `gcloud auth application-default login`. Si vous travaillez depuis un terminal local sur votre propre machine, exécutez `gcloud auth login` et `gcloud auth application-default login` avant `source <(./bin/lab-ctl env)`.

2. Initialisez l'état de départ du Module 1 et lancez une première vérification à blanc :

   ```bash
   ./bin/lab-ctl setup --day=1 --module=1
   ./bin/lab-ctl verify --day=1 --module=1
   ```

   > **Observation** : `lab-ctl verify` retourne un score initial de **`0 / 100`** (`[FAIL]` sur `M1-01` à `M1-04`) avec des indices de remédiation, et met immédiatement à jour votre tableau de bord personnel (isolé pour votre compte) :
   > - **Page Principale (Résultat du dernier test/commande)** : `$STUDENT_DASHBOARD_URL` (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/index.html`)
   > - **Page Logs History (Historique complet de vos journaux)** : `$STUDENT_LOGS_HISTORY_URL` (`https://storage.cloud.google.com/telp-sec-central-791737865143/dashboard/students/${STUDENT_EMAIL}/history.html`)

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

   > **Pourquoi aucune API de charge de travail n'est activée sur `$NET_PROJECT_ID` et `$SEC_PROJECT_ID` à cette étape ?**
   > Conformément au principe de **moindre activation des services (Least Service Enablement)**, une API ne doit être activée sur un projet qu'au moment où une ressource de ce projet en a effectivement besoin :
   > - **`$NET_PROJECT_ID` (`telp-<account-id>-net`)** n'est **pas utilisé au Jour 1** (la VM `bastion` utilise temporairement un VPC d'amorçage local dans `$BACK_PROJECT_ID`). Le projet `$NET_PROJECT_ID` n'entrera en jeu qu'au **Jour 2 (Modules 3 & 4)** lors de la mise en place du **Shared VPC Host**, de Cloud NAT et de Private Cloud DNS.
   > - **`$SEC_PROJECT_ID` (`telp-<account-id>-sec`)** n'est **pas utilisé au Jour 1** (les données utilisent le chiffrement par défaut Google-managed). Il n'entrera en jeu qu'au **Jour 3 (Module 6)** pour héberger les clés **Cloud KMS CMEK** (`kr-central/key-app-storage`), le bucket verrouillé des journaux d'audit (`gs://$SEC_PROJECT_ID-audit-logs`) et les alertes Cloud Monitoring.
   > - Activer prématurément des API sur `$NET_PROJECT_ID` ou `$SEC_PROJECT_ID` (ou activer des API des Jours 2 à 4 comme `dns`, `iap`, `secretmanager`, `cloudkms`, `securitycenter`, `dlp`, `modelarmor`, `aiplatform`) élargirait inutilement la surface d'attaque et fera échouer le contrôle **`M1-01`**.

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

2. **Test de cas limite (Résidence des données en Europe `constraints/gcp.resourceLocations`)** — Tentez de créer un bucket aux États-Unis (`us-central1`) :

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

3. **Vérifier l'accès à votre application *TelP Secure Shop* (Architecture du Module 1)** :
   * **Récupérer vos URLs de service Cloud Run (`FRONTEND_URL` et `BACKEND_URL`)** :
     ```bash
     export FRONTEND_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
     export BACKEND_URL="$(gcloud run services describe backend --project="${BACK_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
     echo "Frontend URL : ${FRONTEND_URL}"
     echo "Backend URL  : ${BACKEND_URL}"
     ```
   * **Option A — Accès Graphique dans le Navigateur (`gcloud run services proxy` + Cloud Shell Web Preview)** :
     La politique d'organisation `constraints/iam.allowedPolicyMemberDomains` interdit `allUsers`, et une URL Cloud Run `.a.run.app` brute attend un en-tête HTTP `Authorization: Bearer <ID_TOKEN>` (elle ne propose pas de mire de connexion OAuth dans le navigateur tant qu'Identity-Aware Proxy n'est pas mis en place au Module 5). Pour ouvrir la boutique dans votre navigateur, démarrez le proxy d'authentification Cloud Run :
     ```bash
     gcloud run services proxy frontend \
       --project="${FRONT_PROJECT_ID}" \
       --region="${REGION}" \
       --port=8080
     ```
     * **Depuis Google Cloud Shell** : cliquez sur le bouton **Web Preview** (en haut à droite de Cloud Shell) $\rightarrow$ **Preview on port 8080** (ce qui ouvre `https://8080-${WEB_HOST}/` pour la boutique et `https://8080-${WEB_HOST}/admin` pour le portail d'administration listant les commandes Firestore).
     * **Depuis un poste local** : ouvrez `http://localhost:8080/` et `http://localhost:8080/admin` dans votre navigateur.
   * **Option B — Vérification de Bout en Bout en Ligne de Commande (`curl` + jeton OIDC)** :
     ```bash
     TOKEN="${LAB_CTL_IDENTITY_TOKEN:-$(gcloud auth print-identity-token)}"
     curl -sS -H "Authorization: Bearer ${TOKEN}" "${BACKEND_URL}/health"
     curl -sS -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/health"

     ORDER_JSON="$(curl -sS -X POST -H "Authorization: Bearer ${TOKEN}" -H "Content-Type: application/json" \
       -d "{\"customer_name\":\"Etudiant Telecom\",\"customer_email\":\"${STUDENT_EMAIL}\",\"amount_eur\":49.90,\"items\":[\"yubikey-5c\"]}" \
       "${FRONTEND_URL}/checkout")"
     echo "${ORDER_JSON}"

     ORDER_ID="$(echo "${ORDER_JSON}" | sed -n 's/.*"order_id":"\([^"]*\)".*/\1/p')"
     curl -sS -H "Authorization: Bearer ${TOKEN}" "${FRONTEND_URL}/receipts/${ORDER_ID}"
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

5. **Comment tester et se connecter en SSH à la VM `bastion` sans adresse IP publique ? (Tunnel TCP IAP)** :
   * Puisque `constraints/compute.vmExternalIpAccess` interdit toute adresse IP publique sur la VM, la connexion SSH s'effectue via **Identity-Aware Proxy (IAP) TCP Forwarding** (`--tunnel-through-iap`), qui encapsule le flux SSH dans HTTPS depuis votre terminal jusqu'aux proxys internes de Google (`35.235.240.0/20`), lesquels joignent l'IP privée (`10.0.1.x:22`) de la VM.
   * **Note sur l'API IAP au Jour 1** : Le transfert TCP IAP vers une VM Compute Engine utilise la permission IAM `iap.tunnelInstances.accessViaIAP` (déjà incluse dans votre rôle `roles/owner` sur `$BACK_PROJECT_ID`) et **ne nécessite pas** d'activer l'API `iap.googleapis.com` (qui ne sera activée qu'au Jour 3 Module 5 pour protéger le Load Balancer HTTP/HTTPS).
   * En revanche, comme `vpc-bootstrap` a été créé en mode personnalisé (`--subnet-mode=custom`), il ne comporte aucune règle de pare-feu entrante par défaut. Créez la règle de pare-feu autorisant `tcp:22` uniquement depuis la plage Google IAP (`35.235.240.0/20`), puis testez la connexion SSH à `bastion` :

   ```bash
   gcloud compute firewall-rules create allow-iap-ssh-bootstrap \
     --project="${BACK_PROJECT_ID}" \
     --network=vpc-bootstrap \
     --direction=INGRESS \
     --action=ALLOW \
     --rules=tcp:22 \
     --source-ranges="35.235.240.0/20"

   gcloud compute ssh bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="europe-west1-b" \
     --tunnel-through-iap \
     --command="hostname && curl -s -H 'Metadata-Flavor: Google' http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/email ; echo"
   ```

   > **Observation de sécurité (à corriger au Module 2)** :
   > - La connexion SSH fonctionne sans aucune adresse IP publique sur `bastion`.
   > - Lors de cette première connexion, `gcloud compute ssh` affiche `Updating project ssh metadata...` car **OS Login** n'est pas encore activé (des clés SSH statiques sont injectées dans les métadonnées).
   > - Le serveur de métadonnées de la VM retourne `<BACK_PROJECT_NUMBER>-compute@developer.gserviceaccount.com` : la VM `bastion` s'exécute avec le compte de service Compute Engine par défaut doté du rôle **`roles/editor`** sur tout le projet `$BACK_PROJECT_ID` ! Vous remédierez à ces deux failles (`sa-bastion` dédié et `enable-oslogin=TRUE`) dans le **Module 2**.

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
