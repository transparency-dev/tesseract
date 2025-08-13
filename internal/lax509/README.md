# lax509

This is a minimalist fork of [`crypto/x509`](https://pkg.go.dev/crypto/x509).

> [!WARNING]
> This library is not safe to use for applications outside of this repository.

> [!WARNING]
> This fork will not be kept in synced with upstream. It will not be updated,
> unless required by a security vulnerability or a critical functionality issue.

## To be or not to be

As specified by [RFC6962 §3.1](https://www.rfc-editor.org/rfc/rfc6962#section-3.1),
CT logs MUST validate submitted chains to ensure that they link up to roots
they accept. `crypto/x509` implements this, and also runs additional common
chain validation checks. However, these additional checks:

- Do not allow chains to contain precertificate or preissuer intermediates.
- Would block non-compliant certificates signed by production roots from being
accepted, thereby preventing them from becoming discoverable.

## The slings and arrows of outrageous fortune

The fork in this directory implements chain verification requirements from
[RFC6962 §3.1](https://www.rfc-editor.org/rfc/rfc6962#section-3.1) and disables
some additional checks, such as:

- **Handling of critical extensions**: CT precertificates are identified by a
critical extension defined in [RFC6962 §3.1](https://www.rfc-editor.org/rfc/rfc6962#section-3.1),
 which the `crypto/x509` library, by design, does not process. A non-processed
 critical extension would fail certificate validation. This check is disabled to
 allow precertificate in the logs.
- **Cert expiry**: `notBefore` and `notAfter` certificate checks are handled at
submission time, based on the `notBeforeLimit` and `notAfterLimit` log
parameters. Therefore, not only we do not need to check them again at
certificate verification time, but we specifically want to accept all
certificates within the `[notBeforeLimit, notAfterLimit]` range, even if they
have expired.
- **CA name restrictions**: an intermediate or root certificate can restrict the
domains it may issue certificates for. This check is disabled to make such
issuances discoverable.
- **Chain length**: this check is confused by chains including preissuer intermediates.
- **Extended Key Usage**: this would ensure that all the EKUs of a child
certificate are also held by its parents. However, the EKU identifying preissuer
intermediate certs in [RFC6962 §3.1](https://www.rfc-editor.org/rfc/rfc6962#section-3.1)
does not need to be set in the issuing certificate, so this check would not pass
for chains using a preissuer intermediate. Also, see <https://github.com/golang/go/issues/24590>.
- **Policy graph validation**: chains that violate policy validation should be
discoverable through CT logs.

## To take arms against a sea of troubles

These additional constraints can be disabled:

- Negative serial numbers are not allowed starting from go1.23. To allow
   them, set `x509negativeserial=1` in the GODEBUG environment variable, either
   in your terminal at build time or with `//go:debug x509negativeserial=1` at
   the top of your main file.
- SHA-1 based signing algorithms are not allowed by default. Set `AcceptSHA1` to
   `true` in the lax509 `VerifyOptions` to allow them, which can be done by
   setting the `accept_sha1_signing_algorithms` TesseraCT flag.
   This is a temporary solution to accept chains issued by Chrome's Merge Delay
   Monitor Root until it stops using SHA-1 based signatures.

## No more; and by a sleep to say we end

We've identified that the following root certificates and chains do not validate
with this library, while they would have validated with the [old CTFE library](https://github.com/google/certificate-transparency-go/tree/master/x509)
used by RFC6962 logs.

If you find any other such chain, [get in touch](/README.md#wave-contact)!

### Roots

- [Jerarquia Entitats de Certificacio Catalanes Root certificate](https://crt.sh/?sha256=88497F01602F3154246AE28C4D5AEF10F1D87EBB76626F4AE0B7F95BA7968799):
This certificate has a negative serial number, which is not allowed starting
from `go1.23`. At the time of writing, this certificate is trusted by the
Microsoft Root store, but not not seem to be used to issue certificates used for
server authentication.
- [Direccion General de Normatividad Mercantil Root certificate](https://crt.sh/?sha256=B41D516A5351D42DEEA191FA6EDF2A67DEE2F36DC969012C76669E616B900DDF):
affected by a known [crypto/x509 issue](https://github.com/golang/go/issues/69463).
This certificate expired on 2025-05-09.

### Chains

Chains that use SHA-1 based signing algorithms such as `sha1WithRSAEncryption`
are not accepted by default. See [To take arms against a sea of troubles](#to-take-arms-against-a-sea-of-troubles)
to allow these chains in.

This signing algorithm [has been rejected by `crypto/x509` since 2020](https://github.com/golang/go/issues/41682),
major CT-enforcing user agents ([Chrome](https://www.chromium.org/Home/chromium-security/education/tls/sha-1/),
[Apple](https://support.apple.com/en-us/103769), [Firefox](https://blog.mozilla.org/security/2017/02/23/the-end-of-sha-1-on-the-public-web/),
[Android](https://developer.android.com/privacy-and-security/security-ssl),
[Microsoft](https://techcommunity.microsoft.com/blog/windows-itpro-blog/microsoft-to-use-sha-2-exclusively-starting-may-9-2021/2261924))
and [CCADB](https://cabforum.org/2014/10/16/ballot-118-sha-1-sunset-passed/)
have been working on deprecating SHA1, for [more than 10 years](https://security.googleblog.com/2014/09/gradually-sunsetting-sha-1.html).

It should not be used. However, it is known to be used by chains issued by these
roots:

- [Chrome's Merge Delay Monitor Root](https://crt.sh/?sha256=86D8219C7E2B6009E37EB14356268489B81379E076E8F372E3DDE8C162A34134):
this is the root used by Chrome to issue test certificate used to monitor CT
logs.
- [Cisco Root CA 2048](https://crt.sh?sha256=8327BC8C9D69947B3DE3C27511537267F59C21B9FA7B613FAFBCCD53B7024000)
such as [this chain](https://crt.sh/?id=284265742).

Given the importance of Chrome's Merge Delay Monitor Root for the CT ecosystem,
we recommend [configuring TesseraCT to allow SHA-1 based signature algorithms](#to-take-arms-against-a-sea-of-troubles)
for the time being.
