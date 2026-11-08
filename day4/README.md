# Jour 4 — Conformité Européenne, Security Command Center (SCC v2) & Sécurité de l'IA Générative (SAIF, Cloud DLP & Model Armor)

Aux **Jours 1 à 3**, vous avez construit l'architecture Zero-Trust de **TelP Secure Shop** sur 4 projets isolés (`net`, `front`, `back`, `sec`) : identités dédiées moindre privilège, segmentation réseau **Shared VPC** et périmètre **VPC Service Controls**, protection périphérique WAF/IAP, chiffrement **Cloud KMS CMEK** inter-projets et puits d'audit **WORM** immuable.

Deux défis majeurs d'exploitation en production restent toutefois à relever lors de cette quatrième et dernière journée :
1. **Dérive de conformité (*Configuration Drift*) & Gestion continue de la posture (CSPM)** : Même dans une organisation gouvernée par des *Organization Policies*, des ressources mal configurées apparaissent au fil du cycle de vie projet — clés cryptographiques CMEK dépourvues de calendrier de rotation automatique, buckets hérités sans *Uniform Bucket-Level Access* ni *Object Versioning*, ou sous-réseaux critiques privés de *VPC Flow Logs*. Comment détecter ces écarts à l'échelle d'un dossier multi-projets avec **Security Command Center (SCC Standard v2)** et **Cloud Asset Inventory**, vérifier la résidence européenne des données, et remédier durablement à la dérive ?
2. **Surface d'attaque de l'IA Générative & Fuite de données personnelles (OWASP Top 10 for LLM / Google SAIF)** : L'équipe produit vient d'intégrer un assistant IA de support client (`ai-assistant`, propulsé par **Gemini 3.5 Flash Lite** sur **Vertex AI**) connecté en *Retrieval-Augmented Generation (RAG)* à la collection Firestore `support_tickets`. Sans garde-fous dédiés, une simple **injection de prompt** permet à un attaquant de contourner les consignes système et d'exfiltrer en clair les IBAN, numéros de carte bancaire, e-mails et téléphones contenus dans les tickets clients !

Au **Jour 4**, vous traitez ces deux volets à travers les **Modules 7 et 8** :

1. **[Module 7 (`day4/mod7`) — Conformité, Résidence des Données Européennes, Security Command Center (SCC v2) & Cloud Asset Inventory](./mod7/README.md)** :
   - Audit en lecture seule (`--effective`) des contraintes d'*Organization Policy* appliquées à votre dossier `folders/${FOLDER_ID}` (`constraints/gcp.resourceLocations`, `constraints/iam.allowedPolicyMemberDomains`, `constraints/storage.uniformBucketLevelAccess`) et distinction architecturale entre **résidence des données** et **souveraineté opérationnelle** (*Cloud Sovereign Solutions* / S3NS / Google Cloud Dedicated).
   - Activation des APIs clientes `securitycenter.googleapis.com` et `cloudasset.googleapis.com` sur votre seul *quota project* SecOps **`$SEC_PROJECT_ID`**, interrogation des *findings* **SCC Findings v2** (`--location=global`, framework intégré **`builtin-security-essentials`** de *Compliance Manager*) et inventaire temps réel **Cloud Asset Inventory**.
   - Remédiation simultanée de 3 dérives de conformité inter-projets : rotation automatique $\le 90$ jours (`7776000s`) sur la clé CMEK `kr-central/key-app-storage` (`$SEC_PROJECT_ID`), activation d'*Uniform Bucket-Level Access* et d'*Object Versioning* sur `gs://${BACK_PROJECT_ID}-compliance-drift` (`$BACK_PROJECT_ID`), et activation des *VPC Flow Logs* sur `subnet-back` (`$NET_PROJECT_ID`).

2. **[Module 8 (`day4/mod8`) — Sécurité de l'IA Générative (SAIF), Sensitive Data Protection (Cloud DLP) & Model Armor](./mod8/README.md)** :
   - Reproduction offensive (*Red Team*) sur le microservice `ai-assistant` initialement non durci : exploitation de `POST /support/chat` pour démontrer l'exfiltration des données bancaires et personnelles du `Ticket #1042` stocké dans Firestore.
   - Déploiement de modèles régionaux **Sensitive Data Protection (Cloud DLP)** (`dlp-inspect-pii` et `dlp-deid-pii` avec pseudonymisation par type `[IBAN_CODE]`, `[CREDIT_CARD_NUMBER]`, `[EMAIL_ADDRESS]`, `[PHONE_NUMBER]`) sur l'endpoint régional `dlp.europe-west1.rep.googleapis.com` afin d'anonymiser les prompts et le contexte RAG **avant** tout appel au modèle de fondation.
   - Déploiement d'une politique **Model Armor** régionale (`telp-ai-guardrail` sur `modelarmor.europe-west1.rep.googleapis.com`) inspectant à la fois les requêtes entrantes (`:sanitizeUserPrompt`) et les réponses du modèle (`:sanitizeModelResponse`) contre les injections de prompt, *jailbreaks*, URI malveillantes et contenus dangereux.
   - Confinement IAM et réseau du service Cloud Run `ai-assistant` : compte de service dédié **`sa-ai-assistant`** doté de 5 rôles de moindre privilège, `--ingress=internal`, attachement Direct VPC sur `subnet-back` (`--vpc-egress=private-ranges-only`), invocation OIDC restreinte exclusivement à `sa-frontend`, et gouvernance de l'endpoint **Vertex AI européen** (`VERTEX_LOCATION=eu`, `https://aiplatform.eu.rep.googleapis.com`).

---

## Cycle de Vie de la Journée 4 & Usage de `./bin/lab-ctl`

1. **Au début du Module 7 (`setup`)** : Exécutez `source <(./bin/lab-ctl env)` puis `./bin/lab-ctl setup --day=4 --module=7` (qui réconcilie l'ingress de `frontend` si le Load Balancer du Jour 3 a été nettoyé, vérifie la présence de `kr-central/key-app-storage` dans `$SEC_PROJECT_ID`, et injecte le bucket en dérive `gs://${BACK_PROJECT_ID}-compliance-drift`).
2. **À la fin du Module 7 (`verify`, PAS de `cleanup` ni de `reset`)** : Validez votre score `100 / 100` avec `./bin/lab-ctl verify --day=4 --module=7`, puis enchaînez directement sur le **Module 8**.
3. **Au début du Module 8 (`setup`)** : Activez les 3 APIs IA/DLP sur `$BACK_PROJECT_ID`, puis exécutez `./bin/lab-ctl setup --day=4 --module=8` (qui sème le document Firestore `support_tickets/ticket-1042`, déploie la version de référence non durcie de `ai-assistant` et câble `AI_ASSISTANT_URL` + la 4e carte interactive HTML « Support Chat IA » sur `frontend`).
4. **En toute fin de Journée 4 (`cleanup --day=4` après validation du Module 8)** : Une fois le **Module 8** validé à **`100 / 100 points`** (`./bin/lab-ctl verify --day=4 --module=8`), exécutez :

```bash
./bin/lab-ctl cleanup --day=4
```

> **Note sur les coûts du Jour 4 (`0,00 $/h` au repos)** : Toutes les ressources configurées aux Modules 7 et 8 (calendrier de rotation KMS logiciel, bucket GCS, *VPC Flow Logs* avec échantillonnage `0.1`, templates Cloud DLP et Model Armor, et services Cloud Run configurés à `min-instances=0` en `--vpc-egress=private-ranges-only` sans passerelle Cloud NAT) ont un coût horaire fixe nul au repos. La commande `./bin/lab-ctl cleanup --day=4` s'assure simplement que la VM `bastion` est arrêtée tout en **préservant intégralement votre posture `100 / 100` sur les Modules 7 et 8**.
