# Local proof authority

The E2B platform uses local attestation as its only proof authority. Run from a clean,
exact commit:

```bash
node scripts/local-proof/local-proof.mjs verify
node scripts/local-proof/local-proof.mjs publish
```

The command binds the receipt to the commit SHA, tree SHA, and a hash of the proof policy
inputs. It runs the bounded authority fixture, Terraform formatting, and Git diff checks;
then it signs the receipt with an SSH key. `publish` posts only the direct `SIG Local Proof`
commit status. GitHub Actions are not consulted and cannot create proof.

Configure `config/local-proof-allowed-signers` locally from the example and keep the private
signing key outside the repository. Missing signer material is a hard failure.

