# Jour 3 — Module 5 : Exposition Sécurisée, Global External Application Load Balancer, WAF Cloud Armor & IAP Zero-Trust

## Objectifs Pédagogiques

À la fin du Jour 2, votre service `backend` est isolé en interne (`--ingress=internal`) derrière votre réseau Shared VPC et votre périmètre VPC Service Controls. En revanche, le service Cloud Run **`frontend`** dans `$FRONT_PROJECT_ID` est toujours exposé directement en `--ingress=all` sur son URL `*.a.run.app`, sans protection WAF contre les attaques applicatives (injections SQL, Cross-Site Scripting, déni de service) ni contrôle d'accès Zero-Trust dédié sur l'interface d'administration `/admin`.

Dans ce module, vous allez :
1. **Architecture Serverless NEG & Routage par Chemin (`M5-01`)** : Encapsuler le service Cloud Run `frontend` dans un **Serverless Network Endpoint Group (`neg-frontend`)** attaché à deux **Backend Services globaux (`EXTERNAL_MANAGED`)** distincts : `be-frontend` (catalogue et commandes clients) et `be-admin` (back-office `/admin` et `/admin/*`) via l'URL Map `url-map-frontend`.
2. **Terminaison HTTPS Globale (`nip.io`) & Verrouillage de l'Ingress Cloud Run (`M5-02`)** : Réserver une adresse IPv4 globale statique (`lb-ip-frontend`), générer un certificat TLS pour le domaine dynamique `<LB_IP>.nip.io`, déployer le proxy HTTPS `https-proxy-frontend` et la règle de transfert `fw-https-frontend` (`:443`), puis verrouiller `frontend` en **`--ingress=internal-and-cloud-load-balancing`** afin d'interdire tout contournement direct du Load Balancer.
3. **Pare-Feu Applicatif Cloud Armor WAF & Anti-DDoS (`M5-03`)** : Constater expérimentalement que des requêtes malveillantes SQLi et XSS atteignent `frontend` avant remédiation (`HTTP 200`), puis attacher une politique **Cloud Armor (`waf-policy-frontend`)** sur `be-frontend` bloquant les injections SQL (`sqli-v33-stable` -> `403`, priorité `1000`), le Cross-Site Scripting (`xss-v33-stable` -> `403`, priorité `1100`) et limitant le débit par IP (`rate-based-ban` à `100 req/min` -> `429`, priorité `2000`).
4. **Accès Administrateur Zero-Trust par Identity-Aware Proxy (`M5-04`)** : Activer **Identity-Aware Proxy (IAP)** sur `be-admin`, autoriser le Service Agent IAP (`service-${FRONT_PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com`) à invoquer Cloud Run `frontend`, accorder `roles/iap.httpsResourceAccessor` exclusivement à votre identité, et vérifier qu'une requête non authentifiée sur `https://<LB_IP>.nip.io/admin` est interceptée en `HTTP 302` vers Google Sign-In.

---

## Étape 1 : Initialisation de l'Environnement et État des Lieux (`0 / 100`)

Depuis **Google Cloud Shell**, placez-vous à la racine du dépôt `telp-sec-course-td`, chargez vos variables d'environnement et préparez le Module 5 :

```bash
source <(./bin/lab-ctl env)
./bin/lab-ctl setup --day=3 --module=5
```

Lancez une première vérification pour observer les 4 contrôles initialement en échec (`0 / 100 points`) :

```bash
./bin/lab-ctl verify --day=3 --module=5
```

---

## Étape 2 : Création du Serverless NEG, des Backend Services Globaux et de l'URL Map (`M5-01` — 25 pts)

### 2.1 Créer le Serverless NEG `neg-frontend`

Dans `$FRONT_PROJECT_ID`, créez un **Serverless Network Endpoint Group** régional (`europe-west1`) pointant vers le service Cloud Run `frontend` :

```bash
gcloud compute network-endpoint-groups create neg-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --region="${REGION}" \
  --network-endpoint-type=serverless \
  --cloud-run-service=frontend
```

### 2.2 Créer les 2 Backend Services globaux (`be-frontend` et `be-admin`) et y attacher `neg-frontend`

Pourquoi créer **deux** Backend Services pointant vers le même Serverless NEG `neg-frontend` ?
Dans l'architecture Global External Application Load Balancer de GCP, les politiques **Cloud Armor** et **Identity-Aware Proxy (IAP)** s'appliquent au niveau d'un **Backend Service**. Séparer `be-frontend` (trafic client général protégé par Cloud Armor) et `be-admin` (routes `/admin` et `/admin/*` protégées par IAP) permet d'exiger une authentification Zero-Trust IAP uniquement sur `/admin` sans bloquer l'accès public au catalogue `/` ou à l'API `/checkout`.

> **Attention (`--global` vs `--network-endpoint-group-region`)** : Un Backend Service `EXTERNAL_MANAGED` est une ressource **globale** (`--global`), tandis que le Serverless NEG `neg-frontend` est une ressource **régionale**. Lors de l'appel à `add-backend`, utilisez `--global` pour cibler le Backend Service et `--network-endpoint-group-region="${REGION}"` pour désigner la région du NEG (n'utilisez **pas** `--region` sur `backend-services`).

```bash
for BE in be-frontend be-admin; do
  gcloud compute backend-services create "${BE}" \
    --project="${FRONT_PROJECT_ID}" \
    --global \
    --load-balancing-scheme=EXTERNAL_MANAGED \
    --protocol=HTTP

  gcloud compute backend-services add-backend "${BE}" \
    --project="${FRONT_PROJECT_ID}" \
    --global \
    --network-endpoint-group=neg-frontend \
    --network-endpoint-group-region="${REGION}"
done
```

### 2.3 Créer l'URL Map `url-map-frontend` et le routage par chemin vers `be-admin`

Créez l'URL Map `url-map-frontend` avec `be-frontend` comme service par défaut, puis ajoutez un `path-matcher` routant `/admin` et `/admin/*` vers `be-admin` :

```bash
gcloud compute url-maps create url-map-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --default-service=be-frontend

gcloud compute url-maps add-path-matcher url-map-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --path-matcher-name=pm-storefront \
  --default-service=be-frontend \
  --path-rules="/admin=be-admin,/admin/*=be-admin" \
  --new-hosts="*"
```

---

## Étape 3 : IP Statique Globale, Certificat TLS (`nip.io`), Proxy HTTPS & Verrouillage de l'Ingress Cloud Run (`M5-02` — 25 pts)

### 3.1 Réserver l'adresse IPv4 globale `lb-ip-frontend`

```bash
gcloud compute addresses create lb-ip-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --ip-version=IPV4

export LB_IP="$(gcloud compute addresses describe lb-ip-frontend --global --project="${FRONT_PROJECT_ID}" --format='value(address)')"
echo "LB_IP = ${LB_IP} (Domaine HTTPS : https://${LB_IP}.nip.io)"
```

### 3.2 Générer le certificat TLS `cert-frontend` et créer le Target HTTPS Proxy & la Forwarding Rule

Identity-Aware Proxy (IAP) et les navigateurs modernes imposent une terminaison **HTTPS (`:443`)**. Le service DNS wildcard public `nip.io` résout automatiquement `<LB_IP>.nip.io` vers `<LB_IP>`. Générez un certificat X.509 pour `${LB_IP}.nip.io` et déployez le proxy HTTPS `https-proxy-frontend` ainsi que la règle de transfert `fw-https-frontend` :

```bash
TMP_TLS="$(mktemp -d)"
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout "${TMP_TLS}/tls.key" \
  -out "/tmp/telp-lb-tls.crt" \
  -subj "/CN=${LB_IP}.nip.io" \
  -addext "subjectAltName=DNS:${LB_IP}.nip.io,IP:${LB_IP}"

gcloud compute ssl-certificates create cert-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --certificate="/tmp/telp-lb-tls.crt" \
  --private-key="${TMP_TLS}/tls.key"
rm -rf "${TMP_TLS}"

gcloud compute target-https-proxies create https-proxy-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --url-map=url-map-frontend \
  --ssl-certificates=cert-frontend

gcloud compute forwarding-rules create fw-https-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --load-balancing-scheme=EXTERNAL_MANAGED \
  --network-tier=PREMIUM \
  --address=lb-ip-frontend \
  --target-https-proxy=https-proxy-frontend \
  --ports=443
```

### 3.3 Verrouiller l'ingress du service Cloud Run `frontend` (`internal-and-cloud-load-balancing`)

Si `frontend` restait en `--ingress=all`, un attaquant pourrait contourner votre Load Balancer (et donc Cloud Armor et IAP) en interrogeant directement l'URL `https://frontend-*.a.run.app`. Restreignez l'ingress de `frontend` à **`internal-and-cloud-load-balancing`** (`INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER`) :

```bash
gcloud run services update frontend \
  --project="${FRONT_PROJECT_ID}" \
  --region="${REGION}" \
  --ingress=internal-and-cloud-load-balancing
```

> **Note de propagation (60 à 90 secondes)** : La programmation initiale d'un nouveau Global Forwarding Rule et de son certificat TLS sur les points de présence Google prend environ 60 à 90 secondes. Testez d'abord que l'accès direct à l'URL `*.run.app` est désormais bloqué (`404`/`403`), puis vérifiez que `https://${LB_IP}.nip.io/health` répond `HTTP 200` :

```bash
export FRONT_RUN_URL="$(gcloud run services describe frontend --project="${FRONT_PROJECT_ID}" --region="${REGION}" --format='value(status.url)')"
export ID_TOKEN="$(gcloud auth print-identity-token)"

echo "1) Test d'accès direct à *.run.app (doit échouer en 404/403 car ingress=internal-and-cloud-load-balancing) :"
curl -s -o /dev/null -w "HTTP %{http_code}\n" -H "Authorization: Bearer ${ID_TOKEN}" "${FRONT_RUN_URL}/health"

echo "2) Attente de disponibilité du Global HTTPS Load Balancer https://${LB_IP}.nip.io/health :"
for i in {1..18}; do
  CODE="$(curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${ID_TOKEN}" "https://${LB_IP}.nip.io/health" || echo "000")"
  echo "   Essai ${i}/18 -> HTTP ${CODE}"
  [[ "${CODE}" == "200" ]] && break
  sleep 5
done
```

Vérifiez à présent que `M5-01` et `M5-02` passent (`50 / 100 points`) :

```bash
./bin/lab-ctl verify --day=3 --module=5
```

---

## Étape 4 : Pare-Feu Applicatif Cloud Armor WAF & Limitation de Débit (`M5-03` — 25 pts)

### 4.1 Constat expérimental avant Cloud Armor : les injections SQLi et XSS passent (`HTTP 200`)

Avant d'attacher une politique Cloud Armor, envoyez une requête contenant une charge utile d'injection SQL (`?q=1' OR '1'='1`) et une charge utile XSS (`?q=<script>alert(1)</script>`) vers le moteur de recherche de la boutique via le Load Balancer :

```bash
export ID_TOKEN="$(gcloud auth print-identity-token)"
curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w "Avant WAF — SQLi : HTTP %{http_code}\n" \
  -H "Authorization: Bearer ${ID_TOKEN}" \
  "https://${LB_IP}.nip.io/?q=1%27%20OR%20%271%27=%271"

curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w "Avant WAF — XSS  : HTTP %{http_code}\n" \
  -H "Authorization: Bearer ${ID_TOKEN}" \
  "https://${LB_IP}.nip.io/?q=%3Cscript%3Ealert(1)%3C/script%3E"
```

Vous observez `HTTP 200` : sans WAF en bordure, les charges d'attaque atteignent directement le conteneur applicatif `frontend`.

### 4.2 Créer la politique Cloud Armor `waf-policy-frontend` et ses 3 règles

Créez la politique de sécurité globale `waf-policy-frontend` dans `$FRONT_PROJECT_ID` avec :
- **Analyse JSON (`--json-parsing=STANDARD`)** : Active le parseur JSON de Cloud Armor afin d'inspecter les valeurs des champs JSON (`POST /checkout`) sans confondre la syntaxe JSON (`{`, `"`, `:`, `}`) avec une attaque SQLi,
- **Priorité `1000`** : Blocage actif (`deny-403`, hors mode preview) des injections SQL via l'ensemble de règles préconfigurées OWASP CRS **`sqli-v33-stable`** au niveau de sensibilité **`{'sensitivity': 1}`** *(Paranoia Level 1 OWASP CRS recommandé pour les APIs JSON afin d'éviter les faux positifs des règles de paranoïa 2–4 `id942260-sqli` / `id942432-sqli` sur les charges utiles JSON légitimes)*,
- **Priorité `1100`** : Blocage actif (`deny-403`, hors mode preview) des attaques Cross-Site Scripting via **`xss-v33-stable`**,
- **Priorité `2000`** *(priorité numériquement supérieure à `1000` et `1100`, donc évaluée **après** les règles WAF afin que les attaques SQLi/XSS soient toujours rejetées en `403` avant le compteur de débit)* : Limitation de débit par IP (`rate-based-ban`) à **`100` requêtes par tranche de `60` secondes** (`exceed-action=deny-429`, `ban-duration-sec=300`),
- Puis attachez `waf-policy-frontend` au Backend Service **`be-frontend`** :

```bash
gcloud compute security-policies create waf-policy-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --description="Cloud Armor WAF (SQLi, XSS) and IP Rate Limiting for TelP Secure Shop"

gcloud compute security-policies update waf-policy-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --json-parsing=STANDARD

gcloud compute security-policies rules create 1000 \
  --project="${FRONT_PROJECT_ID}" \
  --security-policy=waf-policy-frontend \
  --expression="evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})" \
  --action=deny-403 \
  --description="Block SQL injection attacks (OWASP CRS)"

gcloud compute security-policies rules create 1100 \
  --project="${FRONT_PROJECT_ID}" \
  --security-policy=waf-policy-frontend \
  --expression="evaluatePreconfiguredWaf('xss-v33-stable')" \
  --action=deny-403 \
  --description="Block Cross-Site Scripting XSS attacks (OWASP CRS)"

gcloud compute security-policies rules create 2000 \
  --project="${FRONT_PROJECT_ID}" \
  --security-policy=waf-policy-frontend \
  --src-ip-ranges="*" \
  --action=rate-based-ban \
  --rate-limit-threshold-count=100 \
  --rate-limit-threshold-interval-sec=60 \
  --ban-duration-sec=300 \
  --conform-action=allow \
  --exceed-action=deny-429 \
  --enforce-on-key=IP \
  --description="Rate limit per client IP (100 req / 60s -> HTTP 429)"

gcloud compute backend-services update be-frontend \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --security-policy=waf-policy-frontend
```

### 4.3 Vérifier expérimentalement le blocage Cloud Armor (`HTTP 403`) vs trafic légitime (`HTTP 200`)

Après ~30 à 60 secondes de propagation de la politique Cloud Armor au niveau des proxies globaux, relancez vos trois requêtes :

```bash
export ID_TOKEN="$(gcloud auth print-identity-token)"
for i in {1..12}; do
  SQLI_CODE="$(curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${ID_TOKEN}" "https://${LB_IP}.nip.io/?q=1%27%20OR%20%271%27=%271")"
  XSS_CODE="$(curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${ID_TOKEN}" "https://${LB_IP}.nip.io/?q=%3Cscript%3Ealert(1)%3C/script%3E")"
  OK_CODE="$(curl -s --cacert /tmp/telp-lb-tls.crt -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${ID_TOKEN}" "https://${LB_IP}.nip.io/?q=yubikey")"
  echo "Essai ${i}/12 -> SQLi=${SQLI_CODE} (attendu 403), XSS=${XSS_CODE} (attendu 403), Légitime=${OK_CODE} (attendu 200)"
  [[ "${SQLI_CODE}" == "403" && "${XSS_CODE}" == "403" && "${OK_CODE}" == "200" ]] && break
  sleep 5
done
```

---

## Étape 5 : Protection Zero-Trust de `/admin` par Identity-Aware Proxy (IAP) (`M5-04` — 25 pts)

### 5.1 Autoriser le Service Agent IAP à invoquer Cloud Run `frontend`

Lorsque IAP intercepte et valide l'identité d'un utilisateur sur `be-admin`, c'est le **Service Agent Google IAP** (`service-${FRONT_PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com`) qui achemine ensuite la requête vers le Serverless NEG et le service Cloud Run `frontend`. Comme `frontend` est déployé avec `--no-allow-unauthenticated`, vous devez accorder **`roles/run.invoker`** à ce Service Agent IAP sur `frontend` :

```bash
gcloud run services add-iam-policy-binding frontend \
  --project="${FRONT_PROJECT_ID}" \
  --region="${REGION}" \
  --member="serviceAccount:service-${FRONT_PROJECT_NUMBER}@gcp-sa-iap.iam.gserviceaccount.com" \
  --role="roles/run.invoker"
```

### 5.2 Activer IAP sur le Backend Service `be-admin` et accorder `roles/iap.httpsResourceAccessor`

Activez IAP en mode **Google-managed OAuth** directement sur le Backend Service global `be-admin` via `gcloud compute backend-services update be-admin --global --iap=enabled` *(n'utilisez pas `gcloud iap web enable`, qui exige des identifiants OAuth personnalisés obsolètes)*, puis accordez **`roles/iap.httpsResourceAccessor`** exclusivement à votre compte `user:${STUDENT_EMAIL}` (sans `allAuthenticatedUsers` ni `domain:gcp.telecom-paris.fr`) :

```bash
gcloud compute backend-services update be-admin \
  --project="${FRONT_PROJECT_ID}" \
  --global \
  --iap=enabled

gcloud iap web add-iam-policy-binding \
  --project="${FRONT_PROJECT_ID}" \
  --resource-type=backend-services \
  --service=be-admin \
  --member="user:${STUDENT_EMAIL}" \
  --role="roles/iap.httpsResourceAccessor"
```

### 5.3 Vérifier l'interception Zero-Trust IAP sur `https://${LB_IP}.nip.io/admin`

Testez une requête non authentifiée vers `/admin` (sans suivre les redirections) : IAP doit intercepter la requête en bordure Google et renvoyer **`HTTP 302`** vers `https://accounts.google.com/o/oauth2/v2/auth...` :

```bash
curl -s --cacert /tmp/telp-lb-tls.crt -I "https://${LB_IP}.nip.io/admin" | head -n 10
```

---

## Étape 6 : Vérification Finale du Module 5 (`100 / 100 points`)

Lancez la vérification officielle du Module 5 :

```bash
./bin/lab-ctl verify --day=3 --module=5
```

Assurez-vous d'obtenir **`Total Score: 100 / 100 points (All Passed: true)`**.

> **Important — Passez directement au Module 6 sans exécuter `cleanup` ni `reset` !**
> Ne lancez **pas** `./bin/lab-ctl cleanup --day=3` maintenant : passez immédiatement au **[Module 6 (`day3/mod6`)](../mod6/README.md)**. Vous n'exécuterez `./bin/lab-ctl cleanup --day=3` qu'une seule fois, **à la toute fin du Module 6**, après avoir validé vos deux modules de la Journée 3.
