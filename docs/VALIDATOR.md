# Devenir validateur ou délégateur

Deux façons de sécuriser ChainGO et de gagner des récompenses (~3 %/an sur le stake total).

## Option 1 — Validateur (≥ 10 000 CGO + un nœud 24/24)

1. **Un nœud en ligne** : voir [DEPLOYMENT.md](DEPLOYMENT.md). Ta clé de validateur est
   la seed dans `--validator-seed` (ou `validator.seed` du datadir en `--dev`).
2. **Staker** depuis le wallet qui correspond à cette clé :
   ```bash
   chaingo stake --from monwallet --amount 10000
   ```
3. C'est tout. Le consensus te tire au sort proportionnellement à ton poids
   (stake + délégations reçues). À chaque bloc que tu proposes : récompense d'émission
   + tips des transactions.

**Hors-ligne ?** Les *rounds de secours* donnent ta place à un autre validateur après
500 ms — la chaîne ne t'attend pas, mais tu ne gagnes rien pendant ce temps. Le slashing
d'inactivité (downtime) est prévu ; l'inactivité prolongée pénalisera le stake.

**Double-signature = slash immédiat.** Si ton nœud précommit deux blocs différents à la
même hauteur (équivocation — typiquement deux instances avec la même clé), la preuve est
incluse on-chain et **5 % de ton stake ET des délégations reçues sont brûlés**
(`slash_double_sign_bps`). Ne fais JAMAIS tourner deux validateurs avec la même seed.

**Sortir** : `chaingo unstake --from monwallet --amount 10000` → fonds en **unbonding
21 jours** (5 min en devnet), puis liquides automatiquement. Si tu retires tout, tes
délégateurs sont automatiquement désengagés (en unbonding aussi, leurs fonds sont saufs).

## Option 2 — Délégateur (dès 1 CGO, aucun nœud à faire tourner)

Tu confies ton poids à un validateur existant et tu touches ta part de SES récompenses,
au pro-rata, à chaque bloc qu'il propose — moins sa commission (10 %).

```bash
# Choisir un validateur :
curl http://localhost:8545/v1/validators
# Déléguer :
chaingo delegate --from monwallet --to cg<validateur> --amount 50
# Suivre tes gains (ils arrivent directement dans ton solde, bloc après bloc) :
chaingo balance monwallet
# Reprendre tes fonds (unbonding 21 j / 5 min devnet) :
chaingo undelegate --from monwallet --to cg<validateur> --amount 50
```

**Tes CGO ne quittent jamais ta propriété** : la délégation est un poids comptable, le
validateur ne peut pas les dépenser. Risque : si le validateur quitte le réseau, tes
fonds passent en unbonding (récupérés après le délai). Et surtout — **le slashing entame
aussi les délégations** : si ton validateur double-signe, tu perds 5 % de ta délégation
(brûlée) en même temps que lui. Choisis un validateur sérieux (un seul nœud, bon uptime).

## Les chiffres (params de la chaîne, visibles sur `GET /v1/fees`)

| Paramètre | Valeur |
|---|---|
| Stake minimum validateur | 10 000 CGO |
| Délégation minimum | 1 CGO |
| Commission du validateur sur les récompenses des délégateurs | 10 % |
| Émission annuelle (sur le stake total) | ~3 % |
| Unbonding | 21 jours (5 min en devnet) |
