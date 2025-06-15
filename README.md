# Iwinswap Token Analyzer

## 1. Introduction

The **Iwinswap Token Analyzer** provides a scalable architecture for analyzing tokens on EVM-compatible blockchains. It operates as a resilient, long-running service capable of monitoring multiple chains concurrently.

The flagship implementation, `erc20analyzer`,:
- Discovers tokens
- Performs detailed fee and gas cost analysis
- Exposes data through a structured JSON API

It integrates with the `iwinswap-token-system`, serving `token.TokenView` objects per configured chain.

This system is:
- Built with modern Go best practices
- Fault-tolerant and production-ready
- Designed for extensibility and high availability

---

## 2. Use Cases & Value Proposition

### For Arbitrage Bots & MEV Searchers

- **Accurate Profitability Calculation**: Query real-time gas and fee data before trade execution.
- **Honeypot Detection**: Automatically identify tokens with malicious tax behavior.
- **Early Opportunity Discovery**: Discover and analyze new tokens faster than manual approaches.

### For DEX Aggregators

- **Precise Quote Generation**: Account for transfer taxes when calculating final swap amounts.
- **Improved Routing Logic**: Avoid high-tax tokens or intelligently incorporate them into pricing.

### For Wallets and Portfolio Trackers

- **Pre-Transaction Warnings**: Notify users of tokens with transfer taxes before sending.
- **Accurate Balance Representation**: Display the true, spendable balance of taxed tokens.

---

## 3. Key Features

- **Multi-Chain Support**: Fully isolated analysis and endpoints per chain (e.g., `/1/erc20/`, `/42161/erc20/`)
- **Resilient by Design**: Issues on one chain do not affect others; services are context-aware and self-healing.
- **Decoupled & Testable**: Uses interfaces and dependency inversion to isolate logic and enable thorough testing.
- **Scalable API**: Clean per-chain routing and optimized data access patterns.
- **Structured Configuration**: Entirely driven by a single, well-structured `config.yaml` file.
- **Production-Grade Reliability**: Includes structured logging, graceful shutdown, and fault-tolerant services.

---

## 4. System Architecture

### Architectural Principles

- **Interfaces First**: Core logic in `internal/erc20analyzer` depends solely on interfaces.
- **Composition Root Pattern**: All concrete implementations and wiring occur in `cmd/analyzer/main.go`.

### Data Flow

1. **Block Subscription**:  
   `iwinswap-eth-block-subscriber` connects to RPC endpoints and emits new `*types.Block` objects.

2. **Log Extraction**:  
   `LiveExtractor` filters blocks using Bloom filters and extracts logs for relevant events.

3. **Volume Analysis**:  
   `VolumeAnalyzer` processes logs to identify high-volume holders and relevant token activity.

4. **Fee & Gas Simulation**:
   - **Initialization**: `ERC20Initializer` retrieves token metadata (name, symbol, decimals).
   - **Simulation**: `ERC20FeeAndGasRequester` uses an Anvil fork to simulate transfers and calculate cost.

5. **Storage**:  
   Token data is persisted in `TokenStore`.

6. **API Exposure**:  
   `ERC20APIService` serves the `TokenStore.View()` data through per-chain endpoints.

---

## 5. Component Breakdown

| Component                  | Description                                                              |
|---------------------------|--------------------------------------------------------------------------|
| `cmd/analyzer/main.go`    | Application entry point and dependency injection                         |
| `internal/config`         | Configuration parsing from `config.yaml`                                 |
| `internal/erc20analyzer`  | Core logic and orchestration interfaces                                  |
| `internal/services`       | HTTP API layer                                                            |
| `internal/initializers`   | Token metadata initializer                                                |
| `internal/fork`           | Transaction simulation using Anvil                                        |
| `internal/logs`           | Log processing (`LiveExtractor`, `VolumeAnalyzer`)                        |

---

## 6. Configuration

### Running the Application

Create a `config.yaml` file in the root directory.

Run the analyzer using:

```bash
go run ./cmd/analyzer -config config.yaml
```

### Sample Configuration

```yaml
api_server:
  addr: ":8080"

chains:
  - name: "Ethereum Mainnet"
    chain_id: 1
    is_enabled: true
    client_manager:
      rpc_urls:
        - "https://mainnet.infura.io/v3/YOUR_INFURA_KEY"
        - "https://eth.public-node.com"
      max_concurrent_requests: 100
    fee_and_gas_requester:
      fork_rpc_url: "http://localhost:8545"
      max_concurrent_requests: 10
    erc20_analyzer:
      min_token_update_interval: "1h"
      fee_and_gas_update_frequency: "5m"
      volume_analyzer:
        expiry_check_frequency: "1m"
        record_stale_duration: "10m"

  - name: "Arbitrum One"
    chain_id: 42161
    is_enabled: true
    client_manager:
      rpc_urls:
        - "https://arbitrum-mainnet.infura.io/v3/YOUR_INFURA_KEY"
      max_concurrent_requests: 100
    fee_and_gas_requester:
      fork_rpc_url: "http://localhost:8546"
      max_concurrent_requests: 10
    erc20_analyzer:
      min_token_update_interval: "30m"
      fee_and_gas_update_frequency: "2m"
      volume_analyzer:
        expiry_check_frequency: "30s"
        record_stale_duration: "5m"
```

---

## 7. Accessing the API

Once running, the API will be available on the configured port. Each chain is accessible via its dedicated endpoint:

- Ethereum Mainnet: `http://localhost:8080/1/erc20/`
- Arbitrum One: `http://localhost:8080/42161/erc20/`
