package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"time"

	"chaingo/internal/shieldedwallet"
	"chaingo/internal/stark"
	"chaingo/internal/types"
	"chaingo/internal/wallet"
)

// cmdShielded : pool blindé (étage 5). Quatre sous-commandes :
//   shield   : dépose des CGO PUBLICS dans le pool (crée une note ; pas de preuve).
//   transfer : dépense une note et en crée une autre, montants CACHÉS (⏳ preuve).
//   unshield : dépense une note ; un montant public sort du pool vers --to (⏳ preuve).
//   info     : état agrégé du pool (via l'API, sans fuite de contenu).
//
// Modèle de note CÔTÉ WALLET : une note est DÉTERMINISTE depuis (seed du wallet,
// note-index) — nk = DeriveNk(seed, idx), rho = DeriveRho(seed, idx). Le montant
// est porté par l'utilisateur (et caché on-chain). Ainsi le wallet peut regénérer
// le secret d'une note pour la dépenser, sans état persistant supplémentaire.
func cmdShielded(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage : chaingo shielded shield|transfer|unshield|info ...")
	}
	switch args[0] {
	case "shield":
		return cmdShieldedShield(args[1:])
	case "transfer":
		return cmdShieldedTransfer(args[1:])
	case "unshield":
		return cmdShieldedUnshield(args[1:])
	case "info":
		return cmdShieldedInfo(args[1:])
	default:
		return fmt.Errorf("sous-commande shielded inconnue %q (shield|transfer|unshield|info)", args[0])
	}
}

// noteFromWallet reconstruit le secret d'une note détenue par le wallet : nk/rho
// dérivés de (seed, index), montant fourni par l'appelant.
func noteFromWallet(seed []byte, index, value uint64) shieldedwallet.Note {
	return shieldedwallet.Note{
		Value: value,
		Nk:    shieldedwallet.DeriveNk(seed, index),
		Rho:   shieldedwallet.DeriveRho(seed, index),
	}
}

// cmdShieldedShield : dépôt PUBLIC -> note. Aucune preuve (shield n'en a pas).
//   chaingo shielded shield --from <wallet> --amount 10 [--note-index N]
func cmdShieldedShield(args []string) error {
	fs := flag.NewFlagSet("shielded shield", flag.ExitOnError)
	from := fs.String("from", "", "wallet émetteur (paie le dépôt + ShieldFee)")
	amount := fs.String("amount", "", "montant CGO à blinder (déposé dans le pool)")
	noteIndex := fs.Uint64("note-index", 0, "index de la note dans le wallet (regénère nk/rho)")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *amount == "" {
		return fmt.Errorf("--from et --amount sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	amt, err := parseAmount(*amount, types.NativeDecimals)
	if err != nil {
		return err
	}
	note := noteFromWallet(kp.Seed, *noteIndex, amt)
	cm := note.CommitmentBytes()

	// Blob de note : opaque au consensus. Dans un wallet complet, ce serait la note
	// chiffrée (ML-KEM) vers le destinataire ; ici, le dépôt est pour soi-même, on
	// publie un marqueur minimal (le secret réel est regénérable depuis la seed).
	noteBlob := []byte(fmt.Sprintf("shield/v1/idx=%d", *noteIndex))

	tx := &types.Transaction{
		Type:             types.TxShield,
		Amount:           amt,
		ShieldCommitment: cm,
		ShieldNote:       noteBlob,
	}
	fmt.Printf("Dépôt blindé : %s CGO -> pool (note #%d)\n", *amount, *noteIndex)
	fmt.Printf("  Engagement (cm) : %s\n", hex.EncodeToString(cm))
	fmt.Println("  ⚠ Conserve --note-index pour pouvoir DÉPENSER cette note plus tard (transfer/unshield).")
	return signAndSubmit(*api, kp, tx)
}

// cmdShieldedTransfer : dépense une note (note-index/value) et en crée une autre
// (out-index) pour un destinataire blindé (--to-tag-seed), Fee BRÛLÉ. Montants
// cachés. GÉNÈRE LA PREUVE (lent, ~90 s).
//   chaingo shielded transfer --from <wallet> --note-index N --value V --fee F --to-tag-seed S [--out-index M]
func cmdShieldedTransfer(args []string) error {
	fs := flag.NewFlagSet("shielded transfer", flag.ExitOnError)
	from := fs.String("from", "", "wallet émetteur (détient la note d'entrée)")
	noteIndex := fs.Uint64("note-index", 0, "index de la note d'entrée à dépenser")
	value := fs.String("value", "", "montant CGO de la note d'entrée (doit correspondre au dépôt)")
	fee := fs.String("fee", "", "frais publics CGO brûlés (montant révélé)")
	toTagSeed := fs.String("to-tag-seed", "", "graine du tag de propriétaire du destinataire (son adresse blindée)")
	outIndex := fs.Uint64("out-index", 1, "index de la note de sortie (aléa rho côté émetteur)")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *value == "" || *fee == "" || *toTagSeed == "" {
		return fmt.Errorf("--from, --value, --fee et --to-tag-seed sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	inVal, err := parseAmount(*value, types.NativeDecimals)
	if err != nil {
		return err
	}
	feeVal, err := parseAmount(*fee, types.NativeDecimals)
	if err != nil {
		return err
	}
	if feeVal > inVal {
		return fmt.Errorf("--fee (%s) > --value (%s)", *fee, *value)
	}
	in := noteFromWallet(kp.Seed, *noteIndex, inVal)
	// Note de sortie : valeur restante, propriétaire = tag dérivé de --to-tag-seed
	// (l'adresse blindée du destinataire), aléa rho côté émetteur (out-index).
	out := shieldedwallet.Note{
		Value: inVal - feeVal,
		Nk:    shieldedwallet.DeriveNk([]byte(*toTagSeed), 0),
		Rho:   shieldedwallet.DeriveRho(kp.Seed, *outIndex),
	}
	commits, err := fetchCommitments(*api)
	if err != nil {
		return err
	}
	public, proof, err := proveShieldedSpend(commits, in, out, feeVal)
	if err != nil {
		return err
	}
	tx := &types.Transaction{
		Type:        types.TxShieldedTransfer,
		SpendProof:  stark.MarshalSpendProof(proof),
		SpendPublic: stark.MarshalSpendNPublic(public),
		ShieldNote:  []byte(fmt.Sprintf("xfer/v1/out=%d", *outIndex)),
	}
	fmt.Printf("Transfert blindé : note #%d (%s CGO) -> note de sortie, Fee %s CGO brûlé\n", *noteIndex, *value, *fee)
	return signAndSubmit(*api, kp, tx)
}

// cmdShieldedUnshield : dépense une note ; le montant public (--amount-out) sort du
// pool vers --to en CGO public. La note de change (out-index) garde le reliquat.
// GÉNÈRE LA PREUVE (lent, ~90 s).
//   chaingo shielded unshield --from <wallet> --note-index N --value V --amount-out A --to <adresse> [--out-index M]
func cmdShieldedUnshield(args []string) error {
	fs := flag.NewFlagSet("shielded unshield", flag.ExitOnError)
	from := fs.String("from", "", "wallet émetteur (détient la note d'entrée)")
	noteIndex := fs.Uint64("note-index", 0, "index de la note d'entrée à dépenser")
	value := fs.String("value", "", "montant CGO de la note d'entrée")
	amountOut := fs.String("amount-out", "", "montant PUBLIC à sortir du pool vers --to")
	to := fs.String("to", "", "adresse/wallet destinataire du montant public")
	outIndex := fs.Uint64("out-index", 1, "index de la note de change (reliquat blindé)")
	pass := fs.String("pass", "", "mot de passe du wallet")
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	if *from == "" || *value == "" || *amountOut == "" || *to == "" {
		return fmt.Errorf("--from, --value, --amount-out et --to sont requis")
	}
	kp, err := wallet.Load(*from, *pass)
	if err != nil {
		return err
	}
	dest, err := resolveAddress(*to)
	if err != nil {
		return err
	}
	inVal, err := parseAmount(*value, types.NativeDecimals)
	if err != nil {
		return err
	}
	outPub, err := parseAmount(*amountOut, types.NativeDecimals)
	if err != nil {
		return err
	}
	if outPub > inVal {
		return fmt.Errorf("--amount-out (%s) > --value (%s)", *amountOut, *value)
	}
	in := noteFromWallet(kp.Seed, *noteIndex, inVal)
	// Note de change : reliquat blindé, propriétaire = l'émetteur lui-même (out-index).
	change := noteFromWallet(kp.Seed, *outIndex, inVal-outPub)
	commits, err := fetchCommitments(*api)
	if err != nil {
		return err
	}
	// Côté circuit, le « fee » public EST le montant qui sort du pool (unshield).
	public, proof, err := proveShieldedSpend(commits, in, change, outPub)
	if err != nil {
		return err
	}
	tx := &types.Transaction{
		Type:        types.TxUnshield,
		To:          dest,
		SpendProof:  stark.MarshalSpendProof(proof),
		SpendPublic: stark.MarshalSpendNPublic(public),
		ShieldNote:  []byte(fmt.Sprintf("unshield/v1/out=%d", *outIndex)),
	}
	fmt.Printf("Désanonymisation : %s CGO publics -> %s ; reliquat blindé en note #%d\n", *amountOut, dest, *outIndex)
	return signAndSubmit(*api, kp, tx)
}

// cmdShieldedInfo affiche l'état agrégé du pool (sans fuite de contenu).
//   chaingo shielded info [--api URL]
func cmdShieldedInfo(args []string) error {
	fs := flag.NewFlagSet("shielded info", flag.ExitOnError)
	api := fs.String("api", defaultAPI, "URL de l'API")
	fs.Parse(args)
	var info struct {
		Enabled    bool   `json:"enabled"`
		ShieldFee  uint64 `json:"shield_fee"`
		Notes      int    `json:"notes"`
		Nullifiers int    `json:"nullifiers"`
		Balance    uint64 `json:"balance"`
		Root       string `json:"root"`
	}
	if err := getJSON(*api+"/v1/shielded", &info); err != nil {
		return err
	}
	poolState := "DÉSACTIVÉ (params.privacy_enabled=false)"
	if info.Enabled {
		poolState = "actif"
	}
	fmt.Printf("Pool blindé : %s\n", poolState)
	fmt.Printf("  Notes (engagements)   : %d\n", info.Notes)
	fmt.Printf("  Nullifiers dépensés   : %d\n", info.Nullifiers)
	fmt.Printf("  Solde verrouillé      : %s CGO\n", formatAmount(info.Balance, types.NativeDecimals))
	fmt.Printf("  Frais réseau (brûlés) : %s CGO / tx blindée\n", formatAmount(info.ShieldFee, types.NativeDecimals))
	if info.Root != "" {
		fmt.Printf("  Racine Merkle         : %s\n", info.Root)
	}
	return nil
}

// fetchCommitments récupère la liste ORDONNÉE des engagements du pool (hex) via
// l'API (?commitments=1). Nécessaire pour reconstruire le chemin de Merkle.
func fetchCommitments(api string) ([][]byte, error) {
	var info struct {
		Enabled     bool     `json:"enabled"`
		Commitments []string `json:"commitments"`
	}
	if err := getJSON(api+"/v1/shielded?commitments=1", &info); err != nil {
		return nil, err
	}
	if !info.Enabled {
		return nil, fmt.Errorf("pool blindé désactivé sur ce réseau (privacy_enabled=false)")
	}
	if len(info.Commitments) == 0 {
		return nil, fmt.Errorf("pool vide : aucune note à dépenser (faire un shield d'abord)")
	}
	out := make([][]byte, 0, len(info.Commitments))
	for _, h := range info.Commitments {
		b, err := hex.DecodeString(h)
		if err != nil {
			return nil, fmt.Errorf("engagement hex invalide : %w", err)
		}
		out = append(out, b)
	}
	return out, nil
}

// proveShieldedSpend construit le témoin (chemin de Merkle aligné sur le pool) et
// GÉNÈRE la preuve zk-STARK. Lent (~90 s) — on l'annonce à l'utilisateur.
func proveShieldedSpend(commits [][]byte, in, out shieldedwallet.Note, fee uint64) (stark.SpendNPublic, stark.AirProof, error) {
	var zp stark.SpendNPublic
	var za stark.AirProof
	w, feeFelt, err := shieldedwallet.BuildWitness(commits, shieldedwallet.SpendPlan{In: in, Out: out, Fee: fee})
	if err != nil {
		return zp, za, err
	}
	fmt.Println("⏳ Génération de la preuve zk-STARK (~2 s)…")
	t0 := time.Now()
	public, proof := stark.ProveSpendN(w, feeFelt)
	fmt.Printf("✓ Preuve générée en %s\n", time.Since(t0).Round(time.Millisecond))
	return public, proof, nil
}
