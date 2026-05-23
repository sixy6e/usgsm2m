# usgs-m2m

A high-performance, type-safe Go client library and command-line tool for the USGS Machine-to-Machine (M2M) API.
Designed for heavy-duty geospatial data processing pipelines, this package simplifies querying, filtering,
and orchestrating the staging of satellite imagery assets (such as Landsat and VIIRS) directly into your processing environments.

## Features

* **Idiomatic Context Support:** Full control over pipeline timeouts and graceful cancellations via native `context.Context`.
* **Automated Session & Auth Lifecycle:** Handles token acquisition, structural auto-login, and token refresh under the hood seamlessly.
* **Resilient Staging Orchestration:** Implements an elegant "wait-first" polling mechanism that abstracts away the complex state machine of the USGS deep-storage retrieval tier.
* **Type-Safe JSON Contracts:** Built around concrete structs that match the native layout of the M2M endpoints
* **Feature-Rich CLI:** Native subcommands for dataset searching, structural metadata discovery, and automated queue staging.

---

## Installation

## Command Line Interface (CLI)

The package include a multi-command CLI utility (usgs-m2m) that exposes
field discovery, searching and downloads.

### Authentication configuration

The CLI reads credentials from your shell environment variables to ensure secure API transactions:

```bash
export USGS_M2M_AUTH_USERNAME="your_username"
export USGS_M2M_AUTH_TOKEN="your_token_or_api_key"
```

Or via TOML eg ".usgs-m2m.toml"

```toml
[auth]
username = "your_username"
token = "your_token"

[defaults]
dataset = "landsat_ot_c2_l1"
concurrency = 6
output\_dir = "./downloads/"
```

## Spatial search and filtering

The **search** subcommand supports dataset targeting, cloud cover constraints, temporal bounds,
and complex metadata filters (like WRS path/row ranges) to locate target scenes.

* Search Landsat C2 L1 across path and row ranges with cloud cover constraints
```bash
./usgs-m2m search -d landsat\_ot\_c2\_l1 -m "WRS Path=90:92" -m "WRS Row=80:82" --cloud 15 --json -l 10
```

* Search a specific path within a strict date window, outputting raw JSON metadata
```bash
./usgs-m2m search -d landsat\_ot\_c2\_l1 -m "WRS Path=92" --start 2026-01-01 --end 2026-03-31 --json
```

* Pinpoint an exact WRS path/row cell intersection
```bash
./usgs-m2m search -d landsat\_ot\_c2\_l1 -m "WRS Path=92" -m "WRS Row=84" --start 2026-01-01 --end 2026-03-31 --json
```

## High-Performance Bulk Downloads

The **download** command kicks off the full staging queue orchestration loop natively, automatically tracking
the assets and blocking until they are delivered to hot storage.

* Trigger an automated download and restore routine via a target delivery system (e.g., dds)
```bash
./usgs-m2m download VIIRS2025176 -d viirs\_atmos --sys dds
```

* Download multiple via their EntityID
```bash
./m2m download LC80920802026143LGN00 LC80920812026143LGN00 -d landsat\_ot\_c2\_l1 --sys ls\_zip
```

* Specify a file containing a list of EntityIDs
```bash
./m2m search -d landsat\_ot\_c2\_l1 -m "WRS Path=90:92" -m "WRS Row=80:82" -l 2 > download-list.txt
./m2m download -f download-list.txt -d landsat\_ot\_c2\_l1 --sys ls\_zip
```

## Metadata Field Discovery

Because querying the USGS M2M API requires precise field names for metadata search arguments,
you can discover all available searchable attributes for a specific dataset using the fields subcommand.

# Output all valid searchable filter blocks and parameters for a dataset in JSON format
./usgs-m2m fields landsat\_ot\_c2\_l1 --json
