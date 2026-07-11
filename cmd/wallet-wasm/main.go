//go:build js && wasm

// Module WebAssembly du wallet : expose au navigateur la génération de clés
// et la SIGNATURE ML-DSA-65 en réutilisant EXACTEMENT le code Go de la
// chaîne (internal/crypto + internal/types). La clé privée ne quitte
// jamais le navigateur — tout se passe côté client.
package main

import (
	"encoding/hex"
	"encoding/json"
	"syscall/js"

	"chaingo/internal/crypto"
	"chaingo/internal/mnemonic"
	"chaingo/internal/types"
)

func result(m map[string]any) any { return js.ValueOf(m) }
func fail(msg string) any         { return js.ValueOf(map[string]any{"error": msg}) }

// chaingoNewWallet() -> {address, seedHex, mnemonic}
func newWallet(this js.Value, args []js.Value) any {
	kp, err := crypto.GenerateKeyPair()
	if err != nil {
		return fail(err.Error())
	}
	phrase, _ := mnemonic.FromSeed(kp.Seed)
	return result(map[string]any{
		"address":  kp.Address(),
		"seedHex":  hex.EncodeToString(kp.Seed),
		"mnemonic": phrase,
	})
}

// chaingoSeedFromMnemonic(mnemonic) -> {seedHex, address}
// Décode une phrase de 24 mots en son seed (checksum vérifié).
func seedFromMnemonic(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return fail("mnemonic required")
	}
	seed, err := mnemonic.ToSeed(args[0].String())
	if err != nil {
		return fail(err.Error())
	}
	return result(map[string]any{
		"seedHex": hex.EncodeToString(seed),
		"address": crypto.FromSeed(seed).Address(),
	})
}

// chaingoMnemonicFromSeed(seedHex) -> {mnemonic}
// Affiche la phrase de récupération d'un seed existant.
func mnemonicFromSeed(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return fail("seed required")
	}
	seed, err := hex.DecodeString(args[0].String())
	if err != nil {
		return fail("invalid seed")
	}
	phrase, err := mnemonic.FromSeed(seed)
	if err != nil {
		return fail(err.Error())
	}
	return result(map[string]any{"mnemonic": phrase})
}

// chaingoAddressFromSeed(seedHex) -> {address}
func addressFromSeed(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return fail("seed required")
	}
	seed, err := hex.DecodeString(args[0].String())
	if err != nil || len(seed) != crypto.Scheme.SeedSize() {
		return fail("invalid seed")
	}
	return result(map[string]any{"address": crypto.FromSeed(seed).Address()})
}

// chaingoSignTransaction(seedHex, txJSON) -> {signed, hash}
// txJSON : transaction non signée (chain_id, type, to, amount, nonce,
// max_base_fee, tip, ...). Le module remplit from / from_pub_key /
// signature et renvoie la transaction signée prête à POSTer sur /v1/tx.
func signTransaction(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return fail("seedHex and tx json required")
	}
	seed, err := hex.DecodeString(args[0].String())
	if err != nil || len(seed) != crypto.Scheme.SeedSize() {
		return fail("invalid seed")
	}
	var tx types.Transaction
	if err := json.Unmarshal([]byte(args[1].String()), &tx); err != nil {
		return fail("invalid tx json: " + err.Error())
	}
	tx.SignWith(crypto.FromSeed(seed))
	out, err := json.Marshal(&tx)
	if err != nil {
		return fail(err.Error())
	}
	return result(map[string]any{"signed": string(out), "hash": tx.Hash()})
}

func main() {
	js.Global().Set("chaingoNewWallet", js.FuncOf(newWallet))
	js.Global().Set("chaingoAddressFromSeed", js.FuncOf(addressFromSeed))
	js.Global().Set("chaingoSeedFromMnemonic", js.FuncOf(seedFromMnemonic))
	js.Global().Set("chaingoMnemonicFromSeed", js.FuncOf(mnemonicFromSeed))
	js.Global().Set("chaingoSignTransaction", js.FuncOf(signTransaction))
	js.Global().Set("chaingoWasmReady", js.ValueOf(true))
	select {} // garder le module vivant
}
