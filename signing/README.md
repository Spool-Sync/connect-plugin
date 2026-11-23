# Signing key

`public_key.pem` is the public half of the RSA key Spool-Sync signs its
releases with. The Prusa Plugin Marketplace holds the same key (fingerprint
`18fe3041eee9fbd0…`), and Prusa Connect checks each release's
`checksums.txt.sig` against it. Check a release by hand with:

```sh
openssl base64 -d -A < checksums.txt.sig > checksums.sig.bin
openssl dgst -sha256 -verify signing/public_key.pem -signature checksums.sig.bin checksums.txt
```

The private half is never committed. It lives in the GitHub environment
`release` (`PRUSA_CONNECT_EXTENSION_SIGNING_KEY`), for the marketplace's
builder, and in the GitLab CI variable `MARKETPLACE_SIGNING_KEY_B64` (masked,
protected), for the archives GitLab CI builds.
