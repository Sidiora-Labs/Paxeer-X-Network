Security parameter choice
-------------------------

The Bcrypt security parameter used for armored private keys is 12. It is the
`BcryptSecurityParameter` variable in [`armor.go`](armor.go), and
`EncryptArmorPrivKey` passes it to `bcrypt.GenerateFromPassword` to derive the
key that encrypts an exported private key.

Given our security model, where an attacker would need to already have access
to a victim's computer and copy the exported key file or the keyring directory
(as opposed to e.g. web authentication), this parameter choice seems
sufficient. Bcrypt always generates a 448-bit key, so the security in practice
is determined by the length and complexity of a user's password and the time
taken to generate a Bcrypt key from their password (which we can choose with
the security parameter). Each increment of the parameter doubles that time.
Users would be well-advised to use difficult-to-guess passwords.

The `file` keyring backend in [`keyring/keyring.go`](keyring/keyring.go) also
uses Bcrypt, with cost 2, to store the hash of the keyring passphrase in the
`keyhash` file.
