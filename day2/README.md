# Jour 2 — Segmentation Réseau Shared VPC & Périmètre VPC Service Controls (VPC-SC)

Au **Jour 1**, vous avez déployé l'application **TelP Secure Shop** (`frontend`, `backend`, Firestore, Cloud Storage et la VM `bastion`) et verrouillé ses identités IAM selon le principe du moindre privilège (`sa-frontend`, `sa-backend`, `sa-bastion`, OIDC inter-services, Uniform Bucket-Level Access et OS Login). Cependant, sur le plan réseau, la VM `bastion` résidait encore sur un VPC local provisoire (`vpc-bootstrap` dans `$BACK_PROJECT_ID`) et les services Cloud Run `frontend` et `backend` communiquaient via le réseau public Google (`--ingress=all`).

Au **Jour 2**, vous mettez en œuvre une **architecture réseau Zero-Trust multi-projets** articulée autour de deux modules complémentaires :

1. **[Module 3 (`day2/mod3`) — Shared VPC Host & Service Projects, Isolation IAM par Sous-Réseau & Pare-Feu IAP](./mod3/README.md)** :
   - Activation du projet réseau dédié **`$NET_PROJECT_ID`** en tant que **Shared VPC Host Project** et rattachement des projets de charges de travail **`$FRONT_PROJECT_ID`** et **`$BACK_PROJECT_ID`** en tant que **Service Projects**.
   - Création du réseau **`vpc-shared`** et de ses 3 sous-réseaux segmentés (`subnet-front` `10.10.1.0/24`, `subnet-back` `10.10.2.0/24`, `subnet-bastion` `10.10.3.0/24`), suppression du réseau provisoire `vpc-bootstrap` et délégation du rôle **`roles/compute.networkUser` exclusivement au niveau de chaque sous-réseau** (jamais au niveau du projet hôte) avec preuve expérimentale du blocage inter-tiers.
   - Durcissement du pare-feu sur `vpc-shared` (`allow-iap-ssh-bastion` ciblant le compte de service `sa-bastion` depuis `35.235.240.0/20`, règle explicite `deny-all-ingress-default` de priorité `65534` pour l'auditabilité des flux rejetés, et zéro règle d'ouverture `0.0.0.0/0`).
   - Rattachement de `frontend` (`subnet-front`) et `backend` (`subnet-back`, verrouillé en `--ingress=internal`) via **Direct VPC Egress (`--vpc-egress=all-traffic`)**, et migration de la VM `bastion` sur `subnet-bastion`.

2. **[Module 4 (`day2/mod4`) — Private Google Access (PGA), Cloud NAT, Private Cloud DNS & Périmètre VPC Service Controls à 4 Projets](./mod4/README.md)** :
   - Diagnostic du timeout de `POST /checkout` provoqué par `--vpc-egress=all-traffic` lorsque **Private Google Access (PGA)** est désactivé, puis activation de **PGA** sur `subnet-front`, `subnet-back` et `subnet-bastion` pour rétablir l'accès privé aux APIs Google et l'invocation interne de `backend` sans passer par Internet.
   - Déploiement d'un **Cloud Router (`router-nat`)** et d'une passerelle **Cloud NAT (`nat-gw`)** avec journalisation complète (`--log-filter=ALL`) sur `vpc-shared` pour permettre aux instances sans IP publique (`bastion`) d'accéder de manière contrôlée et tracée aux dépôts externes (`https://deb.debian.org`).
   - Création d'une zone **Cloud DNS privée (`zone-internal` — `internal.telp.local.`)** liée à `vpc-shared` et vérification de la résolution DNS interne depuis `bastion`.
   - Enceinte des **4 projets étudiants (`net`, `front`, `back`, `sec`)** dans un **périmètre VPC Service Controls (`perim_student_${PERIM_SLUG}`)** sous votre politique d'accès déléguée **`$ACCESS_POLICY_ID`**, protégeant `storage.googleapis.com`, `firestore.googleapis.com` et `cloudkms.googleapis.com` contre toute exfiltration vers des projets externes.

---

## Cycle de Vie de la Journée 2 & Usage de `./bin/lab-ctl` (`setup`, `verify`, `cleanup`, `reset`)

1. **Au début du Module 3 (`setup`)** : Exécutez `source <(./bin/lab-ctl env)` puis `./bin/lab-ctl setup --day=2 --module=3` (qui active les API réseau du Jour 2 et prépare les Service Agents Cloud Run).
2. **À la fin du Module 3 (`verify`, PAS de `cleanup` ni de `reset` !)** : Validez votre score `100 / 100` avec `./bin/lab-ctl verify --day=2 --module=3`, puis enchaînez **directement** sur le **Module 4** sans exécuter `cleanup --day=2` (car le Module 4 s'appuie immédiatement sur `vpc-shared`, ses 3 sous-réseaux et la VM `bastion` en cours d'exécution).
3. **Au début du Module 4 (`setup`)** : Exécutez `./bin/lab-ctl setup --day=2 --module=4` (qui s'assure que `bastion` est démarrée et active les API Cloud DNS et Access Context Manager).
4. **En toute fin de Journée 2 uniquement (`cleanup --day=2` après le Module 4)** : Une fois le **Module 4** validé à **`100 / 100 points`** (`./bin/lab-ctl verify --day=2 --module=4`), exécutez :

```bash
./bin/lab-ctl cleanup --day=2
```

> **Quand utiliser `cleanup` vs `reset` vs `setup` ?**
> - **`setup --day=2 --module=<3|4>`** : À exécuter systématiquement **au début** de chaque module pour préparer son état initial.
> - **`cleanup --day=2`** : À exécuter **uniquement à la fin du 2e module de la journée (Module 4)**, toujours **après** `./bin/lab-ctl verify --day=2 --module=4`, car `cleanup --day=2` arrête la VM `bastion` et supprime la passerelle `nat-gw` ainsi que le routeur `router-nat` (facturés à l'heure) entre deux séances de cours.
> - **`reset --day=2 --module=<3|4>`** : **Optionnel (uniquement en cas d'erreur)**. Ne l'exécutez jamais lors d'une progression normale ; utilisez-le seulement si vous souhaitez annuler vos modifications d'un module pour le recommencer à zéro (suivi de `./bin/lab-ctl setup --day=2 --module=<3|4>`).
