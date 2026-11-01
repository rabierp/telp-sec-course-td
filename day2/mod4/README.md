# Jour 2 — Module 4 : Private Google Access (PGA), Cloud NAT, Private Cloud DNS & Périmètre VPC Service Controls (VPC-SC)

* **Durée estimée** : 2h00
* **Région cible** : `europe-west1` (Zone : `europe-west1-b`)
* **Projets utilisés** :
  * `$NET_PROJECT_ID` (`telp-<account-id>-net`) : activation de **Private Google Access (PGA)** sur les 3 sous-réseaux, déploiement du **Cloud Router (`router-nat`)** + **Cloud NAT (`nat-gw`)** et de la zone **Cloud DNS privée (`zone-internal`)**.
  * `$FRONT_PROJECT_ID`, `$BACK_PROJECT_ID`, `$NET_PROJECT_ID` et `$SEC_PROJECT_ID` : inclusion des **4 projets** dans votre périmètre **VPC Service Controls (`perim_student_${ACCOUNT_ID}`)** sous la politique d'accès déléguée **`$ACCESS_POLICY_ID`**.

---

## 1. Objectifs Pédagogiques

À la fin du **Module 3**, vos microservices Cloud Run (`frontend` et `backend`) routent l'intégralité de leur trafic sortant (`--vpc-egress=all-traffic`) dans les sous-réseaux `subnet-front` et `subnet-back` de `vpc-shared`, `backend` n'accepte plus que le trafic interne (`--ingress=internal`), et la VM `bastion` tourne sur `subnet-bastion` sans IP publique. Parce que **Private Google Access (PGA)** était désactivé et qu'aucune passerelle **Cloud NAT** n'existait :
* `frontend` ne peut plus joindre `BACKEND_URL` (`*.a.run.app`), et `backend` ne peut plus joindre Firestore ni Cloud Storage (`POST /checkout` échoue en timeout `502`/`504`).
* La VM `bastion` ne peut ni appeler les APIs Google en privé, ni télécharger des paquets de sécurité depuis Internet (`https://deb.debian.org`).
* Enfin, si un attaquant dérobait un jeton valide de `sa-backend` ou d'un développeur, rien ne l'empêcherait au niveau réseau de copier les reçus de `gs://$BACK_PROJECT_ID-receipts` vers un bucket GCS externe à votre environnement.

Dans ce **Module 4**, vous allez :
1. **Activer Private Google Access (PGA) sur les 3 sous-réseaux (`M4-01`)** : Permettre aux charges de travail sans IP publique (`frontend`, `backend`, `bastion`) d'atteindre les APIs Google et les services Cloud Run internes (`--ingress=internal`) sans traverser Internet ni consommer de passerelle NAT.
2. **Déployer Cloud Router & Cloud NAT avec journalisation complète (`M4-02`)** : Créer `router-nat` et `nat-gw` sur `vpc-shared` (`--nat-all-subnet-ip-ranges`, `--enable-logging`, `--log-filter=ALL`) pour fournir une sortie Internet sortante contrôlée et auditée à `bastion`.
3. **Configurer une zone Cloud DNS privée (`M4-03`)** : Créer la zone privée `zone-internal` (`internal.telp.local.`) rattachée à `vpc-shared` et vérifier la résolution interne d'un enregistrement `A` (`bastion.internal.telp.local.` -> `10.10.3.2`).
4. **Enfermer vos 4 projets dans un périmètre VPC Service Controls (`M4-04`)** : Créer l'Access Level `al_student_${ACCOUNT_ID}` et le périmètre régulier `perim_student_${ACCOUNT_ID}` sous votre politique déléguée `$ACCESS_POLICY_ID`, protégeant `storage.googleapis.com`, `firestore.googleapis.com` et `cloudkms.googleapis.com`, puis tester concrètement le blocage d'une tentative d'exfiltration de données vers un bucket externe.

---

## 2. Architecture Cible du Module 4

```
+==========================================================================================+
| Périmètre VPC Service Controls : perim_student_${ACCOUNT_ID} (Policy: $ACCESS_POLICY_ID) |
| Projets protégés (4) : $NET_PROJECT_NUMBER, $FRONT_PROJECT_NUMBER,                       |
|                        $BACK_PROJECT_NUMBER, $SEC_PROJECT_NUMBER                         |
| APIs restreintes : storage.googleapis.com, firestore.googleapis.com, cloudkms            |
| Access Level (al_student_${ACCOUNT_ID}) : <etudiant>@gcp.telecom-paris.fr + instructeur  |
|                                                                                          |
|  +------------------------------------------------------------------------------------+  |
|  | Projet Réseau Hôte ($NET_PROJECT_ID) — vpc-shared                                  |  |
|  |  - Sous-réseaux : subnet-front, subnet-back, subnet-bastion (PGA = ACTIVÉ)         |  |
|  |  - Cloud Router (router-nat) + Cloud NAT (nat-gw, Journalisation = ALL)            |  |
|  |  - Cloud DNS Privé : zone-internal (internal.telp.local.)                          |  |
|  +------------------------------------------------------------------------------------+  |
|           |                                   |                                          |
|           v (PGA interne)                     v (PGA interne + Cloud NAT sortant)        |
|  +---------------------------------+ +------------------------------------------------+  |
|  | Front ($FRONT_PROJECT_ID)       | | Back ($BACK_PROJECT_ID) & Sec ($SEC_...)       |  |
|  |  Cloud Run : frontend           | |  Cloud Run : backend | GCE : bastion           |  |
|  |  Appelle backend.a.run.app via  | |  Accède à Firestore & GCS via PGA à            |  |
|  |  subnet-front (PGA)             | |  l'intérieur du périmètre VPC-SC               |  |
|  +---------------------------------+ +------------------------------------------------+  |
+==========================================================================================+
```

---

## 3. Étape 1 — Initialisation du Module 4 (`setup`), Activation des APIs & Reproduction de la Panne Sans PGA

1. Depuis **Google Cloud Shell**, chargez vos variables d'environnement (`$ACCOUNT_ID`, `$FOLDER_ID`, `$ACCESS_POLICY_ID`, et vos 4 projets), initialisez le Module 4 et lancez un premier diagnostic :

   ```bash
   source <(./bin/lab-ctl env)
   echo "Compte  : ${STUDENT_EMAIL} (ACCOUNT_ID=${ACCOUNT_ID}) | Politique ACM : ${ACCESS_POLICY_ID}"
   ./bin/lab-ctl setup --day=2 --module=4
   ./bin/lab-ctl verify --day=2 --module=4
   ```

2. Activez l'API **Cloud DNS** (`dns.googleapis.com`) et **Access Context Manager** (`accesscontextmanager.googleapis.com`) sur `$NET_PROJECT_ID` (ainsi que `accesscontextmanager.googleapis.com` sur `$BACK_PROJECT_ID`, projet de quota par défaut utilisé par `gcloud access-context-manager`) :

   ```bash
   gcloud services enable dns.googleapis.com accesscontextmanager.googleapis.com logging.googleapis.com \
     --project="${NET_PROJECT_ID}"
   gcloud services enable accesscontextmanager.googleapis.com \
     --project="${BACK_PROJECT_ID}"
   ```

3. **Reproduction du timeout de `POST /checkout` lorsque Private Google Access est désactivé** — Testez une commande sur `frontend` :

   ```bash
   export FRONTEND_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format="value(status.url)")"
   TOKEN="${LAB_CTL_IDENTITY_TOKEN:-$(gcloud auth print-identity-token)}"

   curl -i -sS --max-time 15 -X POST "${FRONTEND_URL}/checkout" \
     -H "Authorization: Bearer ${TOKEN}" \
     -H "Content-Type: application/json" \
     -d '{"customer_name":"Alice Telecom","customer_email":"alice@gcp.telecom-paris.fr","amount_eur":42.50,"items":["Shared-VPC Guide"]}' || true
   ```

   > **Pourquoi l'appel échoue-t-il (`502` / `504` ou timeout) ?**
   > Avec `--vpc-egress=all-traffic`, `frontend` envoie sa requête vers `${BACKEND_URL}` (`*.a.run.app`) à travers `subnet-front`, et `backend` envoie ses requêtes vers `firestore.googleapis.com` et `storage.googleapis.com` à travers `subnet-back`. Tant que `Private Google Access` est désactivé sur ces sous-réseaux, le routeur virtuel de `vpc-shared` rejette les paquets destinés aux adresses IP des APIs Google et du Google Front End à destination de services internes.

---

## 4. Étape 2 — Activation de Private Google Access (PGA) & Rétablissement de `POST /checkout` (`M4-01`)

> **Pourquoi activer PGA sur les 3 sous-réseaux (`subnet-front`, `subnet-back`, `subnet-bastion`) ?**
> - **`subnet-front`** : indispensable pour que `frontend` (avec `--vpc-egress=all-traffic`) puisse invoquer `backend` lorsque `backend` est configuré en **`--ingress=internal`**. Un service Cloud Run `--ingress=internal` n'accepte que les requêtes arrivant via Private Google Access depuis le VPC autorisé (si le trafic sortait par une IP publique Cloud NAT, Google Front End le considérerait comme du trafic Internet externe et le bloquerait en `403/404`).
> - **`subnet-back`** : indispensable pour que `backend` joigne `firestore.googleapis.com` et `storage.googleapis.com` en privé.
> - **`subnet-bastion`** : indispensable pour que la VM `bastion` (`--no-address`) envoie ses journaux et métriques (`logging.googleapis.com`, `monitoring.googleapis.com`) sans passer par Cloud NAT.

1. Activez **Private Google Access** sur les 3 sous-réseaux de `vpc-shared` dans `$NET_PROJECT_ID` :

   ```bash
   for SUBNET in subnet-front subnet-back subnet-bastion; do
     gcloud compute networks subnets update "${SUBNET}" \
       --project="${NET_PROJECT_ID}" \
       --region="${REGION}" \
       --enable-private-ip-google-access
   done
   ```

2. **Vérification immédiate (avant même de créer Cloud NAT !)** — Relancez l'appel `POST /checkout` :

   ```bash
   curl -i -sS -X POST "${FRONTEND_URL}/checkout" \
     -H "Authorization: Bearer ${TOKEN}" \
     -H "Content-Type: application/json" \
     -d '{"customer_name":"Alice Telecom","customer_email":"alice@gcp.telecom-paris.fr","amount_eur":42.50,"items":["Shared-VPC Guide"]}'
   ```

   > **Observation clé** : La commande retourne immédiatement **`HTTP/2 201 Created`** ! L'activation de **Private Google Access (gratuit)** suffit à rétablir l'ensemble des flux vers les APIs Google et entre services Cloud Run internes, sans consommer le moindre octet sur une passerelle Cloud NAT.

---

## 5. Étape 3 — Déploiement de Cloud Router & Cloud NAT avec Journalisation (`M4-02`)

Si Private Google Access couvre les APIs Google, il ne route **pas** le trafic vers les serveurs externes sur Internet (comme les dépôts Linux `https://deb.debian.org`).

1. **Avant Cloud NAT** — Vérifiez depuis la VM `bastion` (`--no-address`) qu'un appel vers un dépôt Internet externe échoue en timeout :

   ```bash
   gcloud compute ssh bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --tunnel-through-iap \
     --command="curl -I -sS --connect-timeout 5 https://deb.debian.org || echo 'Timeout attendu sans Cloud NAT'"
   ```

2. Créez le **Cloud Router (`router-nat`)** et la passerelle **Cloud NAT (`nat-gw`)** sur `vpc-shared` dans `$NET_PROJECT_ID` avec la journalisation complète des traductions d'adresses (`--enable-logging --log-filter=ALL`) :

   ```bash
   gcloud compute routers create router-nat \
     --project="${NET_PROJECT_ID}" \
     --region="${REGION}" \
     --network=vpc-shared

   gcloud compute routers nats create nat-gw \
     --project="${NET_PROJECT_ID}" \
     --region="${REGION}" \
     --router=router-nat \
     --auto-allocate-nat-external-ips \
     --nat-all-subnet-ip-ranges \
     --enable-logging \
     --log-filter=ALL
   ```

3. **Après Cloud NAT** — Relancez le test depuis `bastion` et inspectez les journaux de traduction NAT dans Cloud Logging :

   ```bash
   gcloud compute ssh bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --tunnel-through-iap \
     --command="curl -I -sS --connect-timeout 5 https://deb.debian.org | head -n 5"

   gcloud logging read 'resource.type="nat_gateway"' \
     --project="${NET_PROJECT_ID}" \
     --limit=5 \
     --format="yaml(jsonPayload.connection)"
   ```

---

## 6. Étape 4 — Zone Cloud DNS Privée (`zone-internal`) sur `vpc-shared` (`M4-03`)

1. Créez la zone **Cloud DNS privée `zone-internal`** (`internal.telp.local.`) dans `$NET_PROJECT_ID`, liée au réseau `vpc-shared`, ainsi qu'un enregistrement `A` `bastion.internal.telp.local.` pointant vers `10.10.3.2` :
   *(Note : Nous passons `--billing-project="${NET_PROJECT_ID}"` car la variable par défaut `CLOUDSDK_BILLING_QUOTA_PROJECT` pointe sur `$BACK_PROJECT_ID`, où l'API `dns.googleapis.com` n'est pas activée).*

   ```bash
   gcloud dns managed-zones create zone-internal \
     --project="${NET_PROJECT_ID}" \
     --billing-project="${NET_PROJECT_ID}" \
     --dns-name="internal.telp.local." \
     --description="Private internal DNS zone for Shared VPC vpc-shared" \
     --visibility=private \
     --networks=vpc-shared

   gcloud dns record-sets create bastion.internal.telp.local. \
     --project="${NET_PROJECT_ID}" \
     --billing-project="${NET_PROJECT_ID}" \
     --zone=zone-internal \
     --type=A \
     --ttl=300 \
     --rrdatas="10.10.3.2"
   ```

   > **Note** : Le contrôle automatisé `M4-03` évalue la présence de la zone privée `zone-internal` (`internal.telp.local.`, `visibility=private`) liée à `vpc-shared`. L'enregistrement `bastion.internal.telp.local.` vous permet de tester concrètement la résolution DNS interne depuis la VM `bastion`.

2. Vérifiez la résolution DNS interne depuis la VM `bastion` :

   ```bash
   gcloud compute ssh bastion \
     --project="${BACK_PROJECT_ID}" \
     --zone="${ZONE}" \
     --tunnel-through-iap \
     --command="getent hosts bastion.internal.telp.local"
   ```

---

## 7. Étape 5 — Périmètre VPC Service Controls (VPC-SC) à 4 Projets & Test d'Anti-Exfiltration (`M4-04`)

> **Pourquoi inclure vos 4 projets (`net`, `front`, `back`, `sec`) et `cloudkms.googleapis.com` dès aujourd'hui ?**
> - Au **Jour 3 (Module 6)**, le bucket `gs://$BACK_PROJECT_ID-receipts` (dans `back`) utilisera une clé de chiffrement CMEK hébergée dans `sec`, et le puits de logs d'audit enverra les journaux vers un bucket WORM dans `sec`. En plaçant dès maintenant les **4 projets** dans le même périmètre VPC-SC (`perim_student_${ACCOUNT_ID}`), toutes les communications internes entre `front`, `back`, `net` et `sec` sont autorisées nativement à l'intérieur du périmètre, tandis que toute tentative de copie vers un projet extérieur est bloquée.
> - **Attention** : Ne restreignez **pas** `artifactregistry.googleapis.com` dans le périmètre VPC-SC, car vos services Cloud Run tirent leurs images conteneurs depuis le registre central du cours (`europe-west1-docker.pkg.dev/telp-admin-central/telp-shop/...`) situé hors de votre périmètre.

1. Utilisez la variable `$ACCOUNT_ID` (exportée automatiquement par `source <(./bin/lab-ctl env)`) pour créer votre **Access Level (`al_student_${ACCOUNT_ID}`)** sous votre politique d'accès déléguée `$ACCESS_POLICY_ID` :
   *(Important : incluez votre compte étudiant ainsi que `user:pascal.rabier@gcp.telecom-paris.fr` afin que vos commandes depuis Cloud Shell et l'audit d'évaluation puissent entrer dans le périmètre).*

   ```bash
   cat <<EOF > /tmp/access-level.yaml
   - members:
     - user:${STUDENT_EMAIL}
     - user:pascal.rabier@gcp.telecom-paris.fr
   EOF

   gcloud access-context-manager levels create "al_student_${ACCOUNT_ID}" \
     --policy="${ACCESS_POLICY_ID}" \
     --title="Student & Instructor Access Level (${ACCOUNT_ID})" \
     --basic-level-spec=/tmp/access-level.yaml
   rm -f /tmp/access-level.yaml
   ```

   > **Attention (Jour 3 — Module 6)** : La commande `gcloud access-context-manager levels update ... --basic-level-spec=...` remplace **intégralement** la liste `members:` existante. Si vous mettez à jour `al_student_${ACCOUNT_ID}` au Jour 3, conservez toujours à la fois `user:${STUDENT_EMAIL}` et `user:pascal.rabier@gcp.telecom-paris.fr` dans le fichier YAML.

2. Créez le **périmètre VPC Service Controls régulier (`perim_student_${ACCOUNT_ID}`)** englobant les numéros de vos **4 projets** (`$NET_PROJECT_NUMBER`, `$FRONT_PROJECT_NUMBER`, `$BACK_PROJECT_NUMBER`, `$SEC_PROJECT_NUMBER`) et restreignant `storage.googleapis.com`, `firestore.googleapis.com` et `cloudkms.googleapis.com` :

   ```bash
   gcloud access-context-manager perimeters create "perim_student_${ACCOUNT_ID}" \
     --policy="${ACCESS_POLICY_ID}" \
     --title="4-Project Student VPC-SC Perimeter (${ACCOUNT_ID})" \
     --perimeter-type=regular \
     --resources="projects/${NET_PROJECT_NUMBER},projects/${FRONT_PROJECT_NUMBER},projects/${BACK_PROJECT_NUMBER},projects/${SEC_PROJECT_NUMBER}" \
     --restricted-services="storage.googleapis.com,firestore.googleapis.com,cloudkms.googleapis.com" \
     --access-levels="al_student_${ACCOUNT_ID}"
   ```

3. **Expérience d'exfiltration inter-périmètres (Ingress autorisé vs. Egress bloqué par VPC-SC)** :
   * Tout d'abord, vérifiez qu'un appel `POST "${FRONTEND_URL}/checkout"` fonctionne toujours (`HTTP/2 201 Created`).
   * Ensuite, accordez temporairement à votre utilisateur le rôle `roles/storage.objectViewer` sur `gs://${BACK_PROJECT_ID}-receipts` (sur un bucket avec UBLA activé, le rôle projet `roles/owner` donne `legacyBucketOwner` qui inclut `list`/`create` mais pas `get`), tentez de copier un reçu depuis votre bucket protégé `gs://${BACK_PROJECT_ID}-receipts` (situé **à l'intérieur** de votre périmètre VPC-SC) vers votre dossier personnel dans le bucket central du cours `gs://telp-sec-central-791737865143/results/${STUDENT_EMAIL}/exfil-test.json` (situé dans le projet central **hors** de votre périmètre, sur lequel votre utilisateur détient également le droit IAM d'écriture `roles/storage.objectCreator`), puis **révoquez immédiatement** ce droit temporaire `roles/storage.objectViewer` afin de préserver la conformité moindre privilège du bucket (`M2-04` et Jour 3 `M6-02`) :

   ```bash
   curl -i -sS -X POST "${FRONTEND_URL}/checkout" \
     -H "Authorization: Bearer ${TOKEN}" \
     -H "Content-Type: application/json" \
     -d '{"customer_name":"Bob VPC-SC","customer_email":"bob@gcp.telecom-paris.fr","amount_eur":99.00,"items":["Zero-Trust Perimeter"]}'

   gcloud storage buckets add-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="user:${STUDENT_EMAIL}" \
     --role="roles/storage.objectViewer"

   RECEIPT_OBJ="$(gcloud storage ls "gs://${BACK_PROJECT_ID}-receipts/receipts/" | head -n 1)"
   echo "Tentative d'exfiltration de ${RECEIPT_OBJ} hors du périmètre VPC-SC..."
   gcloud storage cp "${RECEIPT_OBJ}" \
     "gs://telp-sec-central-791737865143/results/${STUDENT_EMAIL}/exfil-test.json" || true

   # Important : révoquez immédiatement le binding temporaire pour maintenir le moindre privilège sur le bucket
   gcloud storage buckets remove-iam-policy-binding "gs://${BACK_PROJECT_ID}-receipts" \
     --member="user:${STUDENT_EMAIL}" \
     --role="roles/storage.objectViewer"
   ```

   > **Résultat attendu (`HTTPError 403: Request is prohibited by organization's policy` — `RESOURCES_NOT_IN_SAME_SERVICE_PERIMETER`)** :
   > Bien que votre identité détienne les permissions IAM sur les deux buckets et figure dans `al_student_${ACCOUNT_ID}` (qui autorise l'**Ingress** depuis votre poste vers le périmètre, mais **pas l'Egress** d'une ressource du périmètre vers un bucket externe), **VPC Service Controls bloque l'exfiltration** !
   > Vous pouvez inspecter la trace d'audit `VPC_SERVICE_CONTROLS` correspondante dans Cloud Audit Logs :
   > ```bash
   > gcloud logging read 'protoPayload.metadata."@type"="type.googleapis.com/google.cloud.audit.VpcServiceControlAuditMetadata"' \
   >   --project="${BACK_PROJECT_ID}" \
   >   --limit=3
   > ```

---

## 8. Étape 6 — Validation Finale du Module 4 (`verify`) & Nettoyage de Fin de Journée (`cleanup`)

1. Lancez la vérification officielle du Module 4 :

   ```bash
   ./bin/lab-ctl verify --day=2 --module=4
   ```

   **Résultat attendu (`100 / 100 points`)** :
   ```text
   ======================================================================
     TelP Cloud Security Verification — Day 2 Module 4 (day2-mod4)
   ======================================================================
     [PASS] M4-01  (25/25 pts) Private Google Access Enabled on All Shared VPC Subnets
     [PASS] M4-02  (25/25 pts) Cloud Router & Cloud NAT Active with Logging
     [PASS] M4-03  (25/25 pts) Private Cloud DNS Zone Bound to vpc-shared
     [PASS] M4-04  (25/25 pts) 4-Project VPC-SC Perimeter Active & App Functional
   ----------------------------------------------------------------------
     Total Score: 100 / 100 points (All Passed: true)
   ======================================================================
   ```

2. Consultez votre tableau de bord personnel mis à jour :
   * **Tableau de bord personnel** : `echo "${STUDENT_DASHBOARD_URL}"`

3. **Nettoyage de fin de Journée 2 (`cleanup --day=2`)** — Le Module 4 étant le **second et dernier module de la Journée 2**, après avoir validé vos `100 / 100 points` sur les Modules 3 et 4, exécutez la commande de nettoyage pour arrêter la VM `bastion` et supprimer la passerelle `nat-gw` (`router-nat`), qui sont facturées à l'heure :

   ```bash
   ./bin/lab-ctl cleanup --day=2
   ```

   > **Rappel (`cleanup` vs `reset`)** :
   > - Exécutez toujours `./bin/lab-ctl verify --day=2 --module=4` **avant** `./bin/lab-ctl cleanup --day=2` (car `cleanup --day=2` supprime `nat-gw` et `router-nat` vérifiés par `M4-02` et arrête `bastion` afin de préserver votre crédit Cloud entre le Jour 2 et le Jour 3 ; votre score `100/100` validé avant le `cleanup` reste quant à lui définitivement acquis sur votre Dashboard).
   > - N'exécutez **pas** `./bin/lab-ctl reset --day=2 --module=4` en fin de journée (`reset` ne sert qu'en cas d'erreur de manipulation pour supprimer le périmètre VPC-SC, le DNS privé et Cloud NAT afin de recommencer le Module 4 à zéro).
