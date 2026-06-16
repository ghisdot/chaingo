//! Contrat compteur — exemple de smart contract WASM pour ChainGO.
//!
//! Démontre l'API hôte d'état : `increment()` lit un compteur dans le stockage
//! du contrat, l'incrémente, le réécrit, et renvoie la nouvelle valeur.
//!
//! Compilation : voir le README. Produit un `.wasm` exécutable par
//! `chaingo wasm run --gas 1000000 counter.wasm increment` (PREVIEW sandbox).

#![no_std]

use core::panic::PanicInfo;

// Fonctions hôtes fournies par ChainGO (module "env"). ABI mémoire : les
// octets sont passés par (pointeur, longueur) dans la mémoire linéaire.
extern "C" {
    /// Lit la valeur d'une clé dans `out` (taille max `max`). Renvoie la
    /// longueur écrite, ou -1 si la clé est absente / le tampon trop petit.
    fn storage_read(kptr: *const u8, klen: u32, out: *mut u8, max: u32) -> i32;
    /// Écrit `val` sous `key` dans le stockage du contrat.
    fn storage_write(kptr: *const u8, klen: u32, vptr: *const u8, vlen: u32);
    /// Journalise une chaîne (visible dans la sortie de `chaingo wasm run`).
    fn log(ptr: *const u8, len: u32);
}

const KEY: &[u8] = b"count";

#[no_mangle]
pub extern "C" fn increment() -> i64 {
    let mut buf = [0u8; 8];
    let n = unsafe { storage_read(KEY.as_ptr(), KEY.len() as u32, buf.as_mut_ptr(), 8) };
    let mut count: i64 = if n == 8 { i64::from_le_bytes(buf) } else { 0 };
    count += 1;
    let bytes = count.to_le_bytes();
    unsafe { storage_write(KEY.as_ptr(), KEY.len() as u32, bytes.as_ptr(), bytes.len() as u32) };
    let msg = b"counter incremented";
    unsafe { log(msg.as_ptr(), msg.len() as u32) };
    count
}

#[panic_handler]
fn panic(_: &PanicInfo) -> ! {
    loop {}
}
