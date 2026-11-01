# Jour 1 — Fondations GCP, Politiques d'Organisation & Durcissement IAM Zero-Trust

Au **Jour 1**, vous prenez en main votre **Landing Zone d'entreprise à 4 projets** (`net`, `front`, `back`, `sec`) sous le dossier `Students/student-<account-id>` (`folders/$FOLDER_ID`) de l'organisation `gcp.telecom-paris.fr` (région `europe-west1`), déployez l'application fil rouge **TelP Secure Shop**, puis durcissez son architecture d'identités IAM :

1. **[Module 1 (`day1/mod1`) — Fondations GCP, Politiques d'Organisation & Déploiement Multi-Projets](./mod1/README.md)** :
   - Découverte de l'isolation multi-projets (`$NET_PROJECT_ID`, `$FRONT_PROJECT_ID`, `$BACK_PROJECT_ID`, `$SEC_PROJECT_ID`), liaison automatique du compte de facturation via `source <(./bin/lab-ctl env)` et application du principe de **moindre activation des API Cloud** (`M1-01`).
   - Observation expérimentale des **Politiques d'Organisation** actives sur le dossier `Students` (`constraints/gcp.resourceLocations`, `constraints/iam.allowedPolicyMemberDomains`, `constraints/storage.publicAccessPrevention`, `constraints/compute.vmExternalIpAccess`, `constraints/compute.requireShieldedVm`).
   - Provisionnement de la base **Firestore Native `(default)`** et du bucket **`gs://$BACK_PROJECT_ID-receipts`** dans `$BACK_PROJECT_ID` (`M1-02`).
   - Déploiement de la version initiale (**v0**) des microservices Cloud Run **`backend`** et **`frontend`** (`M1-03`) ainsi que de la VM de diagnostic **`bastion`** (`e2-micro` sans IP publique `--no-address`, accessible en SSH via tunnel TCP IAP) (`M1-04`).

2. **[Module 2 (`day1/mod2`) — IAM Moindre Privilège, Comptes de Service Dédiés & OIDC Inter-Projets](./mod2/README.md)** :
   - Création des 3 comptes de service dédiés (`sa-frontend`, `sa-backend`, `sa-bastion`), rattachement aux charges de travail et révocation du rôle primitif `roles/editor` sur les comptes de service Compute Engine par défaut (`M2-01`).
   - Mise en œuvre de l'**authentification OIDC inter-projets de service à service** (`USE_OIDC_AUTH=true` sur `frontend` et restriction de `roles/run.invoker` sur `backend` exclusivement à `sa-frontend`) (`M2-02`).
   - Activation d'**Uniform Bucket-Level Access (UBLA)** sur `gs://$BACK_PROJECT_ID-receipts` et application du moindre privilège sur les données (`roles/storage.objectAdmin` ou `roles/storage.objectUser` borné au bucket, `roles/datastore.user` pour `sa-backend`, et rôles de télémétrie `logWriter`/`metricWriter` pour `sa-bastion`) (`M2-03`).
   - Audit et suppression des comptes de service obsolètes (`sa-legacy-export`) et de toute clé statique `USER_MANAGED` sur les 4 projets, activation d'**OS Login** (`enable-oslogin=TRUE`) au niveau du projet `$BACK_PROJECT_ID` et nettoyage des clés SSH statiques résiduelles (`M2-04`).

---

## Cycle de Vie de la Journée 1 & Usage de `./bin/lab-ctl` (`setup`, `verify`, `cleanup`, `reset`)

1. **Au début du Module 1 (`setup`)** : Exécutez `source <(./bin/lab-ctl env)` puis `./bin/lab-ctl setup --day=1 --module=1` pour initialiser votre environnement du Module 1.
2. **À la fin du Module 1 (`verify`, PAS de `cleanup` ni de `reset` !)** : Validez votre score `100 / 100` avec `./bin/lab-ctl verify --day=1 --module=1`, puis enchaînez **directement** sur le **Module 2** sans exécuter `cleanup --day=1` (car le Module 2 réutilise et durcit en place la VM `bastion` en cours d'exécution ainsi que les services Cloud Run du Module 1).
3. **Au début du Module 2 (`setup`)** : Exécutez `./bin/lab-ctl setup --day=1 --module=2` (qui vérifie que `bastion` est démarrée et injecte le compte de service historique inutilisé `sa-legacy-export` à auditer dans `M2-04`).
4. **En toute fin de Journée 1 uniquement (`cleanup --day=1` après le Module 2)** : Une fois le **Module 2** validé à **`100 / 100 points`** (`./bin/lab-ctl verify --day=1 --module=2`), exécutez :

```bash
./bin/lab-ctl cleanup --day=1
```

> **Quand utiliser `cleanup` vs `reset` vs `setup` ?**
> - **`setup --day=1 --module=<1|2>`** : À exécuter systématiquement **au début** de chaque module pour préparer son état initial.
> - **`cleanup --day=1`** : À exécuter **uniquement à la fin du 2e module de la journée (Module 2)** pour arrêter la VM `bastion` (facturée à l'heure) entre le Jour 1 et le Jour 2 tout en conservant vos configurations IAM, Cloud Run, Firestore et GCS.
> - **`reset --day=1 --module=<1|2>`** : **Optionnel (uniquement en cas d'erreur)**. Ne l'exécutez jamais lors d'une progression normale ; utilisez-le seulement si vous avez fait une erreur de configuration et souhaitez remettre un module dans son état initial pour le recommencer (suivi de `./bin/lab-ctl setup --day=1 --module=<1|2>`).
