# argon2

```
// Package argon2 implements the key derivation function Argon2.
// Argon2 was selected as the winner of the Password Hashing Competition and can
// be used to derive cryptographic keys from passwords.
//
// For a detailed specification of Argon2 see [1].
//
// If you aren't sure which function you need, use Argon2id (IDKey) and
// the parameter recommendations for your scenario.
//
//
// Argon2i
//
// Argon2i (implemented by Key) is the side-channel resistant version of Argon2.
// It uses data-independent memory access, which is preferred for password
// hashing and password-based key derivation. Argon2i requires more passes over
// memory than Argon2id to protect from trade-off attacks. The recommended
// parameters (taken from [2]) for non-interactive operations are time=3 and to
// use the maximum available memory.
//
//
// Argon2id
//
// Argon2id (implemented by IDKey) is a hybrid version of Argon2 combining
// Argon2i and Argon2d. It uses data-independent memory access for the first
// half of the first iteration over the memory and data-dependent memory access
// for the rest. Argon2id is side-channel resistant and provides better brute-
// force cost savings due to time-memory tradeoffs than Argon2i. The recommended
// parameters for non-interactive operations (taken from [2]) are time=1 and to
// use the maximum available memory.
//
// [1] https://github.com/P-H-C/phc-winner-argon2/blob/master/argon2-specs.pdf
// [2] https://tools.ietf.org/html/draft-irtf-cfrg-argon2-03#section-9.3
//
```

Forked from golang.org/x/crypto/argon2 with following modifications

```
// Modified to be used with MinIO. Modification here specifically adds
// sync.Pool reusable buffers to avoid large memory build up with frequent
// allocations done by memory hard PBKDF.
```
