# Build and break things

Network infra engineer. I build tools that check whether the obvious
explanation is actually true, and a few systems that just got to work.

## Featured

### [A.V.E.N.G.E](https://github.com/casablanque-code/avenge): edge vibration analytics

Go-based edge computing engine for real-time vibration analytics, inline signal compression, and predictive anomaly detection. Zero external runtime dependencies, zero-heap-allocation hot path.
FFT → SDT compression → Z-score anomaly detection.
MQTT → ClickHouse → Grafana, one `docker compose up` and u got the core stack.

## Products

- [**railcast**](https://github.com/casablanque-code/railcast): Sparkle is the updater. Railcast is the backend it needs. [`live`](https://railcast.casablanque.com)
- [**komposer**](https://github.com/casablanque-code/komposer): terminal UI for Docker Compose files with spec validation and security checks.
- [**platform-infra**](https://github.com/casablanque-code/platform-infra): self-service infrastructure for small teams on Cloudflare Workers + D1 + R2 & gh runners.

## Investigations

> friction → investigation → hypothesis → tool

- [**gai**](https://github.com/casablanque-code/gai): `dig` working doesn't mean your process resolves the name the same way. Reconstructs the real `getaddrinfo()` decision path.
- [**khm**](https://github.com/casablanque-code/khm): `known_hosts` is a database of server identities, not a cache.
- [**cfzt**](https://github.com/casablanque-code/cfzt): one-command Cloudflare Zero Trust Tunnel cli.
- [**sidecheck**](https://github.com/casablanque-code/sidecheck): remote timing side-channel detection using statistics instead of a stopwatch.
- [**burnafterread**](https://github.com/casablanque-code/burnafterread): share a secret without giving the server access to it. [`live`](https://burnafterread.casablanque.com/)
- [**netconv**](https://github.com/casablanque-code/netconv): config conversion is about semantics, not replacing keywords.

### Network forensics

Packets are evidence, not truth. Everything runs in the browser; PCAPs never leave your machine.

[ospf](https://ospf.postmortem.casablanque.com/) · [dhcp](https://dhcp.postmortem.casablanque.com/) · [stp](https://stp.postmortem.casablanque.com/) · [dns](https://dns.postmortem.casablanque.com/)

### Published

<p align="left">
  <a href="https://crates.io/crates/sidecheck"><img alt="sidecheck" src="https://img.shields.io/crates/v/sidecheck?label=sidecheck&style=flat-square"></a>
  <a href="https://crates.io/crates/sidecheck-core"><img alt="sidecheck-core" src="https://img.shields.io/crates/v/sidecheck-core?label=sidecheck-core&style=flat-square"></a>
  <a href="https://crates.io/crates/gai-probe"><img alt="gai-probe" src="https://img.shields.io/crates/v/gai-probe?label=gai-probe&style=flat-square"></a>
  <a href="https://crates.io/crates/gai-core"><img alt="gai-core" src="https://img.shields.io/crates/v/gai-core?label=gai-core&style=flat-square"></a>
  <a href="https://crates.io/crates/pcap-frame-parser"><img alt="pcap-frame-parser" src="https://img.shields.io/crates/v/pcap-frame-parser?label=pcap-frame-parser&style=flat-square"></a>
  <a href="https://crates.io/crates/ios-config"><img alt="ios-config" src="https://img.shields.io/crates/v/ios-config?label=ios-config&style=flat-square"></a>
  <a href="https://www.npmjs.com/package/burnafter"><img alt="burnafter" src="https://img.shields.io/npm/v/burnafter?label=burnafter&style=flat-square"></a>
  <a href="https://www.npmjs.com/package/@lighthouse-explorer/light-parser"><img alt="light-parser" src="https://img.shields.io/npm/v/@lighthouse-explorer/light-parser?label=light-parser&style=flat-square"></a>
</p>

---

Zero Trust ain't just for infra. Don't trust this file either, verify it.

<details>
<summary>Verify this README</summary>

```
gpg --keyserver keys.openpgp.org --recv-keys 2A49CD4AF95F76DF38FC1F9EBFB3563375149683
curl -sO https://raw.githubusercontent.com/casablanque-code/casablanque-code/main/README.md
curl -sO https://raw.githubusercontent.com/casablanque-code/casablanque-code/main/README.md.sig
gpg --verify README.md.sig README.md
```

</details>

---

<p align="left">
  <a href="mailto:casablanque@proton.me"><img alt="reach me out" src="https://img.shields.io/badge/reach%20me%20out-casablanque%40proton.me-6d4aff?style=flat-square&logo=protonmail&logoColor=white"></a>
  <a href="https://keys.openpgp.org/search?q=2A49CD4AF95F76DF38FC1F9EBFB3563375149683"><img alt="PGP" src="https://img.shields.io/badge/PGP-0x75149683-2e7d32?style=flat-square&logo=gnuprivacyguard&logoColor=white"></a>
  <a href="https://ko-fi.com/casablanque"><img alt="Ko-fi" src="https://img.shields.io/badge/Ko--fi-support-ff5e5b?style=flat-square&logo=kofi&logoColor=white"></a>
</p>
