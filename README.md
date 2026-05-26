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

This project uses [Mage](https://magefile.org/) for automated build and installation tasks. Make sure you have [Go](https://go.dev/doc/install) installed on your machine before proceeding.

### 1. Install Mage
If you don't have the Mage build tool installed yet, you can fetch it quickly via Go:

```bash
go install [github.com/magefile/mage@latest](https://github.com/magefile/mage@latest)
```

### 2. Clone and Install

```bash
git clone [https://github.com/sixy6e/usgsm2m.git](https://github.com/sixy6e/usgsm2m.git)
cd usgsm2m
```

### 3. Compile and install the tool

```bash
mage install
```

### 4.

* Check the help for usgs-m2m

```bash
usgs-m2m is a CLI tool for downloading USGS datasets like Landsat, MODIS, and VIIRS

Usage:
  usgs-m2m [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  download    Download scenes from USGS M2M using a generic dataset catalog
  fields      List queryable metadata fields for a specific dataset
  filters     List searchable filter constraints for a dataset
  help        Help about any command
  search      Query USGS metadata registries

Flags:
  -h, --help              help for usgs-m2m
  -j, --json              Output command results in JSON format
      --token string      USGS M2M API Token
      --username string   USGS M2M Username

Use "usgs-m2m [command] --help" for more information about a command.
```

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
output_dir = "./downloads/"
```

## Spatial search and filtering of scenes

The **search** subcommand supports dataset targeting, cloud cover constraints, temporal bounds,
and complex metadata filters (like WRS path/row ranges) to locate target scenes.

* Search Landsat C2 L1 across path and row ranges with cloud cover constraints
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 -m "WRS Path=90:92" -m "WRS Row=80:82" --cloud 15 --json -l 10
```

* Search a specific path within a strict date window, outputting raw JSON metadata
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 -m "WRS Path=92" --start 2026-01-01 --end 2026-03-31 --json
```

* Pinpoint an exact WRS path/row cell intersection
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 -m "WRS Path=92" -m "WRS Row=84" --start 2026-01-01 --end 2026-03-31 --json
```

* Search using a GeoJSON formatted file
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 --geojson test_aoi2.geojson
```

* Search using a bounding box
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 --bbox "146.0,-34.9,146.2,-34.7"
```

## Search the catalog for available datasets
The search dataset command can be used to discover what datasets can be searched within via the
search scene subcommand.
```bash
./usgs-m2m search dataset --json
```

## High-Performance Bulk Downloads

The **download** command kicks off the full staging queue orchestration loop natively, automatically tracking
the assets and blocking until they are delivered to hot storage.

* Trigger an automated download and restore routine via a target delivery system (e.g., dds)
```bash
./usgs-m2m download VIIRS2025176 -d viirs_atmos --sys dds
```

* Download multiple via their EntityID
```bash
./usgs-m2m download LC80920802026143LGN00 LC80920812026143LGN00 -d landsat_ot_c2_l1 --sys ls_zip
```

* Specify a file containing a list of EntityIDs
```bash
./usgs-m2m search scene -d landsat_ot_c2_l1 -m "WRS Path=90:92" -m "WRS Row=80:82" -l 2 > download-list.txt
./usgs-m2m download -f download-list.txt -d landsat_ot_c2_l1 --sys ls_zip
```

## Metadata Field Discovery

Because querying the USGS M2M API requires precise field names for metadata search arguments,
you can discover all available searchable attributes for a specific dataset using the fields subcommand.

* Output all valid searchable filter blocks and parameters for a dataset in JSON format

```bash
./usgs-m2m fields landsat_ot_c2_l1 --json
```
