# Jour 2 — Module 3 : Shared VPC Host & Service Projects, Isolation IAM par Sous-Réseau & Pare-Feu IAP

* **Durée estimée** : 2h00
* **Région cible** : `europe-west1` (Zone : `europe-west1-b`)
* **Projets utilisés** :
  * `$NET_PROJECT_ID` (`telp-<account-id>-net`) : **Shared VPC Host Project** hébergeant le réseau centralisé `vpc-shared`, les 3 sous-réseaux (`subnet-front`, `subnet-back`, `subnet-bastion`) et les règles de pare-feu.
  * `$FRONT_PROJECT_ID` (`telp-<account-id>-front`) : **Shared VPC Service Project** hébergeant le microservice `frontend` rattaché à `subnet-front`.
  * `$BACK_PROJECT_ID` (`telp-<account-id>-back`) : **Shared VPC Service Project** hébergeant le microservice `backend` (rattaché à `subnet-back` en mode `--ingress=internal`) et la VM `bastion` (rattachée à `subnet-bastion`).
* **Projet non utilisé à cette étape** :
  * `$SEC_PROJECT_ID` (`telp-<account-id>-sec`) : réservé au Jour 3 (Module 6 — KMS & Audit WORM) ; l'API `compute.googleapis.com` doit rester **désactivée** sur `$SEC_PROJECT_ID`.

---

## 1. Objectifs Pédagogiques

Dans une Landing Zone d'entreprise sur Google Cloud, les équipes applicatives (`front`, `back`) ne gèrent pas leurs propres réseaux VPC ni leurs propres règles de pare-feu : l'administration réseau est centralisée dans un **Projet Hôte Shared VPC (`$NET_PROJECT_ID`)**, tandis que les projets applicatifs consomment uniquement les sous-réseaux qui leur sont explicitement délégués.

Dans ce **Module 3**, vous allez :
1. **Activer le Shared VPC Host & supprimer les réseaux locaux (`M3-01`)** : Transformer `$NET_PROJECT_ID` en projet hôte Shared VPC (`XPN Host`), y associer `$FRONT_PROJECT_ID` et `$BACK_PROJECT_ID` comme projets de service, créer le VPC personnalisé `vpc-shared` et ses 3 sous-réseaux (`10.10.1.0/24`, `10.10.2.0/24`, `10.10.3.0/24`), et supprimer le réseau provisoire `vpc-bootstrap` du Jour 1.
2. **Appliquer le moindre privilège réseau par sous-réseau (`M3-02`)** : Accorder le rôle `roles/compute.networkUser` aux **Service Agents Google** et aux comptes de service **au niveau de chaque sous-réseau individuel** (`subnet-front`, `subnet-back`, `subnet-bastion`) — et **jamais** au niveau du projet hôte `$NET_PROJECT_ID` — puis vérifier expérimentalement qu'un service du projet `front` ne peut pas s'attacher au sous-réseau `subnet-back`.
3. **Verrouiller le pare-feu du Shared VPC (`M3-03`)** : Créer la règle d'entrée `allow-iap-ssh-bastion` autorisant `tcp:22` uniquement depuis la plage Identity-Aware Proxy (`35.235.240.0/20`) vers le compte de service cible `sa-bastion@$BACK_PROJECT_ID.iam.gserviceaccount.com`, ainsi qu'une règle explicite de rejet par défaut journalisée `deny-all-ingress-default` (priorité `65534`), sans aucune règle d'ouverture `0.0.0.0/0`.
4. **Rattacher les charges de travail au Shared VPC & isoler l'entrée du `backend` (`M3-04`)** : Configurer **Direct VPC Egress (`--vpc-egress=all-traffic`)** sur `frontend` (`subnet-front`) et `backend` (`subnet-back`), restreindre `backend` à `--ingress=internal` (`INGRESS_TRAFFIC_INTERNAL_ONLY`), et recréer la VM `bastion` sur `subnet-bastion` sans IP publique (`--no-address`).

---

## 2. Architecture Cible du Module 3

```
+---------------------------------------------------------------------------------------+
| Projet Réseau Hôte ($NET_PROJECT_ID) — Shared VPC Host (XPN)                          |
| VPC Personnalisé : vpc-shared                                                         |
| Pare-feu : allow-iap-ssh-bastion (35.235.240.0/20 -> tcp:22 -> sa-bastion)            |
|            deny-all-ingress-default (priorité 65534, DENY all, journalisé)            |
|                                                                                       |
|  +-------------------------+  +-------------------------+  +-----------------------+  |
|  | subnet-front            |  | subnet-back             |  | subnet-bastion        |  |
|  | 10.10.1.0/24            |  | 10.10.2.0/24            |  | 10.10.3.0/24          |  |
|  | IAM: networkUser UNIQT  |  | IAM: networkUser UNIQT  |  | IAM: networkUser UNIQT|  |
|  | pour $FRONT_PROJECT_ID  |  | pour $BACK_PROJECT_ID   |  | pour $BACK_PROJECT_ID |  |
|  +------------+------------+  +------------+------------+  +-----------+-----------+  |
+---------------|----------------------------|---------------------------|--------------+
                | Direct VPC Egress          | Direct VPC Egress         | NIC0 (Sans IP pub)
                | (all-traffic)              | (all-traffic)             | SSH via Tunnel IAP
                v                            v                           v
+-------------------------------+  +-------------------------------+  +-----------------+
| Service Project : front       |  | Service Project : back        |  | Service : back  |
|  Cloud Run : frontend         |  |  Cloud Run : backend          |  |  GCE : bastion  |
|  SA : sa-frontend             |  |  Ingress : INTERNAL_ONLY      |  |  SA : sa-bastion|
+-------------------------------+  +-------------------------------+  +-----------------+
```

---

## 3. Étape 1 — Initialisation du Module 3 (`env`, `setup`) & Activation des APIs Réseau

1. Depuis **Google Cloud Shell**, chargez vos variables d'environnement (`$ACCOUNT_ID`, `$FOLDER_ID`, `$ACCESS_POLICY_ID`, et vos 4 projets) et préparez le Module 3 :

   ```bash
   source <(./bin/lab-ctl env)
   echo "Compte  : ${STUDENT_EMAIL} (ACCOUNT_ID=${ACCOUNT_ID})"
   ./bin/lab-ctl setup --day=2 --module=3
   ./bin/lab-ctl verify --day=2 --module=3
   ```

   > **Observation** : Les 4 contrôles (`M3-01` à `M3-04`) sont initialement en échec (`[FAIL]`, `0 / 100 points`).

2. Activez l'API Compute Engine (`compute.googleapis.com`) sur `$NET_PROJECT_ID` et `$FRONT_PROJECT_ID` (ainsi que `iap.googleapis.com` sur `$BACK_PROJECT_ID`), et initialisez l'identité du **Service Agent Cloud Run** sur `$FRONT_PROJECT_ID` et `$BACK_PROJECT_ID` :

   ```bash
   gcloud services enable compute.googleapis.com --project="${NET_PROJECT_ID}"
   gcloud services enable compute.googleapis.com run.googleapis.com --project="${FRONT_PROJECT_ID}"
   gcloud services enable compute.googleapis.com iap.googleapis.com run.googleapis.com --project="${BACK_PROJECT_ID}"

   gcloud beta services identity create --service=run.googleapis.com --project="${FRONT_PROJECT_ID}" || true
   gcloud beta services identity create --service=run.googleapis.com --project="${BACK_PROJECT_ID}" || true

   gcloud projects add-iam-policy-binding "${FRONT_PROJECT_ID}" \
     --member="serviceAccount:service-${FRONT_PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com" \
     --role="roles/run.serviceAgent" --quiet >/dev/null
   gcloud projects add-iam-policy-binding "${BACK_PROJECT_ID}" \
     --member="serviceAccount:service-${BACK_PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com" \
     --role="roles/run.serviceAgent" --quiet >/dev/null
   ```

   > **Note — Pourquoi `gcloud beta services identity create` ?** : Sur un projet GCP où aucun connecteur réseau Cloud Run n'a encore été utilisé, le Service Agent `service-<PROJECT_NUMBER>@serverless-robot-prod.iam.gserviceaccount.com` peut être provisionné de manière paresseuse (*lazy*). Forcer sa création dès maintenant vous permet de lui accorder `roles/compute.networkUser` sur les sous-réseaux partagés à l'Étape 3 avant même la mise à jour des services Cloud Run.
   >
   > **Attention** : N'activez **pas** `compute.googleapis.com` sur `$SEC_PROJECT_ID` (le projet SecOps ne doit héberger aucune instance ni aucun réseau VPC).

---

## 4. Étape 2 — Création de `vpc-shared`, des 3 Sous-Réseaux & Activation du Shared VPC (`M3-01`)

1. Activez le mode **Shared VPC Host** sur `$NET_PROJECT_ID` et associez `$FRONT_PROJECT_ID` et `$BACK_PROJECT_ID` comme **Service Projects** :

   ```bash
   gcloud compute shared-vpc enable "${NET_PROJECT_ID}"

   gcloud compute shared-vpc associated-projects add "${FRONT_PROJECT_ID}" \
     --host-project="${NET_PROJECT_ID}"

   gcloud compute shared-vpc associated-projects add "${BACK_PROJECT_ID}" \
     --host-project="${NET_PROJECT_ID}"
   ```

2. Créez le réseau personnalisé **`vpc-shared`** et ses 3 sous-réseaux régionaux (`europe-west1`) dans `$NET_PROJECT_ID` :
   *(Note pédagogique : Nous créons volontairement ces sous-réseaux avec `--no-enable-private-ip-google-access` et `--no-enable-flow-logs` afin d'observer en fin de Module 3 l'impact de l'absence de Private Google Access sur Direct VPC Egress, que vous résoudrez au Module 4, et de corriger les Flow Logs via Security Command Center au Module 7).*

   ```bash
   gcloud compute networks create vpc-shared \
     --project="${NET_PROJECT_ID}" \
     --subnet-mode=custom \
     --bgp-routing-mode=regional

   gcloud compute networks subnets create subnet-front \
     --project="${NET_PROJECT_ID}" \
     --network=vpc-shared \
     --region="${REGION}" \
     --range="10.10.1.0/24" \
     --no-enable-private-ip-google-access \
     --no-enable-flow-logs

   gcloud compute networks subnets create subnet-back \
     --project="${NET_PROJECT_ID}" \
     --network=vpc-shared \
     --region="${REGION}" \
     --range="10.10.2.0/24" \
     --no-enable-private-ip-google-access \
     --no-enable-flow-logs

   gcloud compute networks subnets create subnet-bastion \
     --project="${NET_PROJECT_ID}" \
     --network=vpc-shared \
     --region="${REGION}" \
     --range="10.10.3.0/24" \
     --no-enable-private-ip-google-access \
     --no-enable-flow-logs
   ```

   > **Note (en cas de réinitialisation via `lab-ctl reset --day=2 --module=3`)** : Si vous aviez déjà attaché vos services Cloud Run à `subnet-front` et `subnet-back` auparavant, Google Cloud Run conserve une réservation d'adresses IP internes (`/28` `PURPOSE: SERVERLESS`) pendant environ 1 heure après le détachement, ce qui empêche la suppression immédiate de `vpc-shared`, `subnet-front` et `subnet-back` lors d'un `reset`. Dans ce cas, ignorez le message `already exists` sur `vpc-shared`, `subnet-front` et `subnet-back` (leurs paramètres PGA/Flow Logs ont déjà été remis à zéro par `reset`) et créez simplement `subnet-bastion`.

---

## 5. Étape 3 — Isolation IAM par Sous-Réseau & Test d'Intrusion Inter-Tiers (`M3-02`)

> **Concept clé — Qui consomme réellement la permission `compute.subnetworks.use` (`roles/compute.networkUser`) ?**
> Lorsque vous déployez un service Cloud Run avec Direct VPC Egress ou une VM Compute Engine dans un **Service Project** (`front` ou `back`), ce n'est pas uniquement le compte de service applicatif (`sa-frontend`, `sa-backend`, `sa-bastion`) qui attache l'interface réseau dans le **Host Project** (`net`), mais le **Service Agent managé par Google Cloud** du projet de service :
> - Pour **Cloud Run Direct VPC Egress** : `service-<PROJECT_NUMBER>@serverless-robot-prod.iam.gserviceaccount.com`
> - Pour **Compute Engine (VM GCE)** : `<PROJECT_NUMBER>@cloudservices.gserviceaccount.com` et `service-<PROJECT_NUMBER>@compute-system.iam.gserviceaccount.com`
>
> En accordant `roles/compute.networkUser` **uniquement sur le sous-réseau concerné** (et jamais au niveau du projet `$NET_PROJECT_ID`), vous garantissez mathématiquement qu'aucune charge de travail du projet `front` ne peut s'attacher à `subnet-back` ou `subnet-bastion`, et inversement.

1. Accordez `roles/compute.networkUser` **exclusivement sous-réseau par sous-réseau** :

   ```bash
   export SA_FRONT="sa-frontend@${FRONT_PROJECT_ID}.iam.gserviceaccount.com"
   export SA_BACK="sa-backend@${BACK_PROJECT_ID}.iam.gserviceaccount.com"
   export SA_BASTION="sa-bastion@${BACK_PROJECT_ID}.iam.gserviceaccount.com"

   # 1. subnet-front : uniquement les agents et SA du projet FRONT
   for member in \
     "serviceAccount:service-${FRONT_PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com" \
     "serviceAccount:${FRONT_PROJECT_NUMBER}@cloudservices.gserviceaccount.com" \
     "serviceAccount:${SA_FRONT}"; do
     gcloud compute networks subnets add-iam-policy-binding subnet-front \
       --project="${NET_PROJECT_ID}" \
       --region="${REGION}" \
       --member="${member}" \
       --role="roles/compute.networkUser"
   done

   # 2. subnet-back : uniquement les agents et SA du projet BACK
   for member in \
     "serviceAccount:service-${BACK_PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com" \
     "serviceAccount:${BACK_PROJECT_NUMBER}@cloudservices.gserviceaccount.com" \
     "serviceAccount:${SA_BACK}"; do
     gcloud compute networks subnets add-iam-policy-binding subnet-back \
       --project="${NET_PROJECT_ID}" \
       --region="${REGION}" \
       --member="${member}" \
       --role="roles/compute.networkUser"
   done

   # 3. subnet-bastion : uniquement les agents Compute Engine du projet BACK, sa-bastion et votre compte étudiant
   for member in \
     "serviceAccount:${BACK_PROJECT_NUMBER}@cloudservices.gserviceaccount.com" \
     "serviceAccount:service-${BACK_PROJECT_NUMBER}@compute-system.iam.gserviceaccount.com" \
     "serviceAccount:${SA_BASTION}" \
     "user:${STUDENT_EMAIL}"; do
     gcloud compute networks subnets add-iam-policy-binding subnet-bastion \
       --project="${NET_PROJECT_ID}" \
       --region="${REGION}" \
       --member="${member}" \
       --role="roles/compute.networkUser"
   done
   ```

   > **Note** : Sur `subnet-bastion`, `user:${STUDENT_EMAIL}` est requis car la commande `gcloud compute instances create` vérifie la permission `compute.subnetworks.use` de l'appelant humain au moment de la création de l'instance dans un Service Project, tandis que `compute-system` et `cloudservices` sont les deux Service Agents Compute Engine susceptibles d'allouer l'interface réseau interne selon l'opération.

2. **Expérience négative (Preuve d'isolation inter-tiers)** — Tentez d'attacher le service `frontend` (du projet `$FRONT_PROJECT_ID`) au sous-réseau `subnet-back` (réservé au projet `$BACK_PROJECT_ID`) :

   ```bash
   gcloud run services update frontend \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --network="projects/${NET_PROJECT_ID}/global/networks/vpc-shared" \
     --subnet="projects/${NET_PROJECT_ID}/regions/${REGION}/subnetworks/subnet-back" \
     --vpc-egress=all-traffic
   ```

   > **Résultat attendu (`PERMISSION_DENIED`)** : Même si votre utilisateur personnel possède `roles/owner` sur `$NET_PROJECT_ID`, le plan de contrôle Cloud Run échoue car le Service Agent `service-${FRONT_PROJECT_NUMBER}@serverless-robot-prod.iam.gserviceaccount.com` ne détient pas `compute.subnetworks.use` sur `subnet-back` !

---

## 6. Étape 4 — Règles de Pare-Feu Moindre Privilège sur `vpc-shared` (`M3-03`)

> **Pourquoi cibler un Compte de Service (`--target-service-accounts`) plutôt qu'un Tag réseau (`--target-tags`) ?**
> Sur GCP, n'importe quel administrateur d'instance dans un projet de service peut ajouter librement un tag réseau arbitraire (`--tags=allow-ssh`) sur une VM. En revanche, attacher le compte de service `sa-bastion@$BACK_PROJECT_ID.iam.gserviceaccount.com` à une VM exige la permission IAM `iam.serviceAccounts.actAs`. Cibler `sa-bastion` dans la règle de pare-feu lie cryptographiquement l'ouverture réseau à l'identité IAM de l'instance.

1. Créez la règle `allow-iap-ssh-bastion` autorisant `tcp:22` uniquement depuis le bloc TCP Forwarding d'Identity-Aware Proxy (`35.235.240.0/20`) vers `sa-bastion`, ainsi que la règle explicite de rejet par défaut `deny-all-ingress-default` (priorité `65534`, juste au-dessus de la règle implicite `65535` afin de pouvoir journaliser les tentatives d'intrusion rejetées) :

   ```bash
   gcloud compute firewall-rules create allow-iap-ssh-bastion \
     --project="${NET_PROJECT_ID}" \
     --network=vpc-shared \
     --direction=INGRESS \
     --priority=1000 \
     --action=ALLOW \
     --rules=tcp:22 \
     --source-ranges="35.235.240.0/20" \
     --target-service-accounts="${SA_BASTION}" \
     --enable-logging

   gcloud compute firewall-rules create deny-all-ingress-default \
     --project="${NET_PROJECT_ID}" \
     --network=vpc-shared \
     --direction=INGRESS \
     --priority=65534 \
     --action=DENY \
     --rules=all \
     --source-ranges="0.0.0.0/0" \
     --enable-logging
   ```

---

## 7. Étape 5 — Migration de `frontend`, `backend` et `bastion` sur `vpc-shared` & Suppression de `vpc-bootstrap` (`M3-01` & `M3-04`)

1. Rattachez **`backend`** à `subnet-back` via **Direct VPC Egress (`--vpc-egress=all-traffic`)** et verrouillez son exposition réseau en **`--ingress=internal`** (`INGRESS_TRAFFIC_INTERNAL_ONLY`) :

   ```bash
   gcloud run services update backend \
     --project="${BACK_PROJECT_ID}" \
     --region="${REGION}" \
     --service-account="${SA_BACK}" \
     --network="projects/${NET_PROJECT_ID}/global/networks/vpc-shared" \
     --subnet="projects/${NET_PROJECT_ID}/regions/${REGION}/subnetworks/subnet-back" \
     --vpc-egress=all-traffic \
     --ingress=internal
   ```

2. Rattachez **`frontend`** à `subnet-front` via **Direct VPC Egress (`--vpc-egress=all-traffic`)** :

   ```bash
   gcloud run services update frontend \
     --project="${FRONT_PROJECT_ID}" \
     --region="${REGION}" \
     --service-account="${SA_FRONT}" \
     --network="projects/${NET_PROJECT_ID}/global/networks/vpc-shared" \
     --subnet="projects/${NET_PROJECT_ID}/regions/${REGION}/subnetworks/subnet-front" \
     --vpc-egress=all-traffic \
     --ingress=all
   ```

3. Supprimez dans l'ordre des dépendances la VM `bastion` et le réseau provisoire `vpc-bootstrap` du Jour 1 dans `$BACK_PROJECT_ID`, puis recréez la VM **`bastion`** directement dans le sous-réseau partagé **`subnet-bastion`** :

   ```bash
   gcloud compute instances delete bastion --project="${BACK_PROJECT_ID}" --zone="${ZONE}" --quiet
   gcloud compute firewall-rules delete allow-iap-ssh-bootstrap --project="${BACK_PROJECT_ID}" --quiet
   gcloud compute networks subnets delete subnet-bootstrap --project="${BACK_PROJECT_ID}" --region="${REGION}" --quiet
   gcloud compute networks delete vpc-bootstrap --project="${BACK_PROJECT_ID}" --quiet

   gcloud compute instances create bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --machine-type=e2-micro \
     --subnet="projects/${NET_PROJECT_ID}/regions/${REGION}/subnetworks/subnet-bastion" \
     --no-address \
     --service-account="${SA_BASTION}" \
     --scopes="https://www.googleapis.com/auth/cloud-platform" \
     --image-family=debian-12 \
     --image-project=debian-cloud \
     --metadata="enable-oslogin=TRUE" \
     --shielded-secure-boot \
     --shielded-vtpm \
     --shielded-integrity-monitoring
   ```

---

## 8. Étape 6 — Expérimentation des Frontières Réseau & Validation du Module 3 (`verify`)

1. **Test SSH via IAP sur le Shared VPC (`subnet-bastion`)** — Vérifiez que la VM `bastion` possède désormais une IP privée dans `10.10.3.0/24` et est joignable via la règle `allow-iap-ssh-bastion` du projet hôte `$NET_PROJECT_ID` :

   ```bash
   gcloud compute ssh bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --tunnel-through-iap \
     --command="hostname -I"
   ```

2. **Preuve du blocage réseau de `backend` (`--ingress=internal`)** — Au Jour 1 (Module 2), un appel direct depuis Cloud Shell vers `BACKEND_URL/health` avec votre propre jeton utilisateur (`roles/owner`) passait car `backend` était en `--ingress=all`. Re-testez maintenant cet appel :

   ```bash
   export BACKEND_URL="$(gcloud run services describe backend --project="${BACK_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   TOKEN="${LAB_CTL_IDENTITY_TOKEN:-$(gcloud auth print-identity-token)}"
   curl -i -s -H "Authorization: Bearer ${TOKEN}" "${BACKEND_URL}/health" | head -n 12
   ```

   > **Résultat attendu (`HTTP/2 404` / `403` par Google Front End)** : La requête est désormais rejetée au niveau réseau avant même l'évaluation IAM, car l'appel ne provient pas du Shared VPC `vpc-shared`.

3. **Observation du "Cliffhanger" vers le Module 4 (`POST /checkout` en timeout sans Private Google Access)** — Lancez la vérification officielle du Module 3 :

   ```bash
   ./bin/lab-ctl verify --day=2 --module=3
   ```

   **Résultat attendu (`100 / 100 points`)** :
   ```text
   ======================================================================
     TelP Cloud Security Verification — Day 2 Module 3 (day2-mod3)
   ======================================================================
     [PASS] M3-01  (25/25 pts) Shared VPC Host & Service Projects Active, Default VPCs Absent
     [PASS] M3-02  (25/25 pts) Subnet-Level IAM Isolation (subnet-front, subnet-back, subnet-bastion)
     [PASS] M3-03  (25/25 pts) IAP SSH Firewall Rule & Zero 0.0.0.0/0 Allow Ingress
     [PASS] M3-04  (25/25 pts) Workloads Bound to Shared VPC & Backend Internal Ingress
   ----------------------------------------------------------------------
     Total Score: 100 / 100 points (All Passed: true)
   ======================================================================
   ```

   > **Transition vers le Module 4 (PAS de `cleanup` ni de `reset` entre les Modules 3 et 4 !)** :
   > - Si vous testez maintenant `POST /checkout` sur `frontend`, vous constaterez que l'appel échoue en **timeout (`502` / `504`)** ! En effet, `--vpc-egress=all-traffic` force tout le trafic sortant de `frontend` (`subnet-front`) et `backend` (`subnet-back`) à traverser `vpc-shared`, or `Private Google Access` est encore désactivé sur les sous-réseaux. Vous allez diagnostiquer et résoudre ce problème dès le début du **[Module 4](../mod4/README.md)**.
   > - **N'exécutez pas `./bin/lab-ctl cleanup --day=2` (ni `reset`) à la fin du Module 3** : enchaînez directement sur le **Module 4**, qui s'appuie sur `vpc-shared` et la VM `bastion` en cours d'exécution. Le `cleanup --day=2` ne s'exécute qu'à la toute fin de la Journée 2, après avoir validé le Module 4 (`reset --day=2 --module=3` n'étant utile qu'en cas d'erreur pour recommencer le Module 3 à zéro).
