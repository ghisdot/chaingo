# Example dApps

Minimal, dependency-free examples of building on ChainGO. Fork them as a
starting point. See the [developer quickstart](../../docs/DEV-QUICKSTART.md).

## `pulse/` — live network dashboard

A single self-contained HTML file that reads a ChainGO node's public REST API
(no keys, read-only) and shows live network stats, recent blocks and an address
balance lookup.

```bash
# Open directly, or serve the folder:
cd examples/dapp/pulse && python3 -m http.server 8080
# → http://localhost:8080  (point it at any node, e.g. https://node.chaingo.org)
```

It demonstrates the read side of the API. For the **write** side (signing &
sending transactions with post-quantum ML-DSA-65 keys), see
[docs/DEV-QUICKSTART.md §3](../../docs/DEV-QUICKSTART.md).
