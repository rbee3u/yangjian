# Tests are fixture-first and offline by default

`go test ./...` runs offline against recorded RPC and Sourcify responses plus golden JSON; live integration tests are gated by `YANGJIAN_LIVE=1`. Acceptance fixtures pin the block number, block hash, and raw provider responses for the Uniswap V3 Pool. Live-only tests would make the suite flaky and network-dependent, while mock-only tests would never catch provider or schema drift. Fixtures make schema changes visible in review and keep the evidence path reproducible; the gated live suite catches the drift that fixtures cannot.
