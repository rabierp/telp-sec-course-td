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

## Maîtrise des Coûts en Fin de Journée 2 (`cleanup --day=2`)

Une fois vos vérifications des Modules 3 et 4 validées à **`100 / 100 points`**, exécutez en fin de séance :

```bash
./bin/lab-ctl cleanup --day=2
```

> **Important** : Exécutez toujours `./bin/lab-ctl verify --day=2 --module=4` **avant** `./bin/lab-ctl cleanup --day=2`, car la commande `cleanup --day=2` arrête la VM `bastion` et supprime la passerelle `nat-gw` ainsi que le routeur `router-nat` afin d'économiser les coûts horaires entre deux séances de cours.
