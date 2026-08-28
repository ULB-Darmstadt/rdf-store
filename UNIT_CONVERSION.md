# QUDT Unit Conversion to SI Base Units

This document describes how QUDT (Quantities, Units, Dimensions, and Data Types) units
are converted to their SI base equivalents.

## Overview

The conversion system works in three phases:

1. **Build-time catalog generation** selects canonical SI units and computes
   conversion factors into an embedded catalog (`units.json`).
2. **Index-time conversion** applies those factors to numeric measurement values
   when indexing RDF resources, so all indexed quantities are stored in their
   canonical SI form. The unit predicate itself is *not* rewritten — the index
   keeps the original unit URI so unit facets show the units actually used in
   the data.
3. **Query-time conversion** translates user-entered filter bounds to SI before
   querying Solr and converts returned facet statistics back to the user's
   selected unit. It is implemented in the frontend and uses conversion factors
   served by the backend's `/quantities` endpoint.

## Dimension Vectors

Every unit has a dimension vector that encodes its SI base dimension composition. The
eight positions correspond to:

| Position | Dimension              | SI Base Unit      |
| -------- | ---------------------- | ----------------- |
| A        | Amount of substance    | mole (mol)        |
| E        | Electric current       | ampere (A)        |
| L        | Length                 | metre (m)         |
| I        | Luminous intensity     | candela (cd)      |
| M        | Mass                   | kilogram (kg)     |
| H        | Thermodynamic temperature | kelvin (K)     |
| T        | Time                   | second (s)        |
| D        | Dimensionless          | —                 |

Each exponent is the power of the corresponding base unit. For example:
- `A0E0L1I0M0H0T0D0` — length (metre)
- `A0E0L0I0M0H1T0D0` — temperature (kelvin)
- `A0E0L2I0M1H0T-2D0` — energy (joule = kg·m²·s⁻²)

Conversion between two units is only permitted when their dimension vectors match.

## Conversion Formula

The general formula for converting a value from unit A to unit B is:

```
value_B = ((value_A + offsetA) × multiplierA) / multiplierB - offsetB
```

Where `multiplier` is the factor that converts one unit to its SI coherent form, and
`offset` is an additive constant for non-ratio scales (e.g. Celsius, Fahrenheit).

Converting directly to an SI base unit (where the target multiplier is 1 and offset is 0)
simplifies to:

```
value_SI = (value + offset) × multiplier
```

### Example Conversion Factors (from `units.json`)

| Unit URI     | Multiplier   | Offset   | Dimension Vector              | Meaning                        |
| ------------ | ------------ | -------- | ----------------------------- | ------------------------------ |
| `K`          | 1            | 0        | `…H1…`                        | kelvin (SI base)               |
| `DEG_C`      | 1            | 273.15   | `…H1…`                        | °C → K: add 273.15            |
| `DEG_F`      | 0.5555…      | 459.67   | `…H1…`                        | °F → K: (F+459.67) × 5/9     |
| `M`          | 1            | 0        | `…L1…`                        | metre (SI base)                |
| `FT`         | 0.3048       | 0        | `…L1…`                        | 1 foot = 0.3048 m             |
| `KiloM`      | 1000         | 0        | `…L1…`                        | kilometre                      |
| `CentiM`     | 0.01         | 0        | `…L1…`                        | centimetre                     |
| `MI`         | 1609.344     | 0        | `…L1…`                        | 1 mile = 1609.344 m           |
| `KiloGM`     | 1            | 0        | `…M1…`                        | kilogram (SI base)             |
| `DAY`        | 86400        | 0        | `…T1…`                        | 1 day = 86400 s               |
| `L`          | 0.001        | 0        | `…L3…`                        | 1 litre = 0.001 m³           |
| `PA`         | 100000       | 0        | `…L-1 M1 T-2…`               | pascal                         |
| `HZ`         | 1            | 0        | `…T-1…`                       | hertz (SI exact match for Frequency) |

## Configuration

Conversion is controlled by three environment variables that specify which RDF
predicates carry the unit, quantity kind, and numeric value on a measurement node:

| Variable             | Purpose                                    | Example                                              |
| -------------------- | ------------------------------------------ | ---------------------------------------------------- |
| `CONVERSION_UNIT`    | predicate linking a node to its QUDT unit  | `http://w3id.org/nfdi4ing/metadata4ing#hasUnit`      |
| `CONVERSION_QUANTITY`| predicate linking a node to its quantity kind | `http://w3id.org/nfdi4ing/metadata4ing#hasKindOfQuantity` |
| `CONVERSION_VALUE`   | predicate linking a node to its numeric value | `http://w3id.org/nfdi4ing/metadata4ing#hasNumericalValue` |

All three must be set for conversion to be active. If any is missing or empty,
the entire conversion system is disabled — `ScanConversionContext` returns `nil`
and no unit conversion takes place. This allows the system to gracefully handle
data models that don't carry quantity information.

There is no explicit on/off toggle; simply omitting the variables is sufficient
to turn conversion off.

The same three values are part of the application configuration (`Config` struct
in `backend/base/config.go`) and are served to the frontend as
`conversionUnit`, `conversionQuantity`, and `conversionValue` via the
`/api/v1/config` endpoint, where they drive query-time conversion.

## Indexing Behavior

During indexing, numeric values on a measurement node whose predicate matches
`CONVERSION_VALUE` are converted to the canonical SI unit of the node's quantity
kind (`qudt.Convert`). The stored value under the unit predicate is left
untouched: the original unit URI (e.g. `…unit/DEG_C`) remains in the index so
that unit facets and filters reflect the units present in the data. Unit URI
canonicalization at index time was removed in favor of converting filter bounds
at query time instead.

## Query-Time Conversion (Filtering)

The frontend module `frontend/src/unit-conversion.ts` converts between the
user's selected unit and SI when building filters and displaying facet
statistics:

- **`UnitConversionResolver`** scans the query's fields and criteria for sibling
  fields whose path ends with the configured value/unit/quantity predicates (plus
  the fixed `qudt:isDeltaQuantity` predicate for delta detection). Fields sharing
  the same subject path form a quantity group. The selected unit and quantity kind
  are taken from `equals` criteria on the group's unit/kind fields; a quantity kind
  can also be learned automatically when a kind facet resolves to exactly one
  bucket (auto-detection).
- Conversions are fetched in a single batch from the backend's `/quantities`
  endpoint and cached per `(quantityKind, unit, isDelta)` triple; lookups that cannot be
  resolved are cached as "no conversion" to avoid repeated requests. If the request
  itself fails, filtering degrades gracefully to unconverted values and nothing is
  cached, so a later request retries.
- **Range criteria** are converted to SI with `(value + offset) × multiplier`
  before they are turned into Solr range filters, matching the index-time
  conversion.
- **Facet statistics** (min/max) returned by Solr in SI are converted back with
  `value / multiplier − offset` for display.
- When a quantity kind is selected, fixed by `sh:hasValue`, or inferred from a
  single facet bucket, its canonical SI unit is inserted into the unit choices
  and selected initially. A shape-fixed unit also selects its canonical target
  when the backend confirms a conversion, including identity conversions such
  as Volt to Volt. This covers canonical units absent from the indexed unit
  buckets, which continue to represent the original units used by the data.
- Unit and quantity-kind fields are pure metadata: once their selection produced a
  resolved conversion, their criteria never restrict search results. If no
  conversion could be resolved, unit criteria remain active as plain filters so a
  unit facet selection still narrows results.

## Delta Quantities

For difference quantities (e.g. a temperature *difference* of 10 °C), both offsets are
zeroed out. This means:

- A difference of 100 °C = a difference of 100 K (not 373.15 K)
- A difference of 50 °F = a difference of 27.78 K

The `isDelta` flag is set when the RDF triple `qudt:isDeltaQuantity` on a measurement
node is `"true"` or `"1"`.

Query-time conversion handles delta quantities as well: when a sibling field on the
`qudt:isDeltaQuantity` predicate is selected (e.g. via an `equals` criterion with
`"true"` or `"1"`), the frontend requests a delta-aware conversion and the
`/quantities` endpoint returns the factors without offsets — matching index-time
behavior.

## Canonical Unit Selection

Each QUDT **quantity kind** (e.g. "Length", "Temperature", "Energy") is mapped to one
**canonical SI unit** at catalog build time. This is the target unit when converting
via `Convert()`.

### Selection Priority

When multiple SI-eligible units exist for a quantity kind, the following priority
determines the canonical choice:

1. **`siExactMatch`** (highest) — Domain-specific canonical units like Hz for Frequency
   or Bq for Activity, even though both have dimension `T-1`.
2. **`SIBase` / `SIDerivedCoherent`** — Standard SI base or coherent derived units.
3. **`SIApplicable` and not `Scaled`** — Compound SI units like m³ for Volume.
4. Everything else — not a candidate.

Only units with `multiplier == 1` and `offset == 0` are eligible. If two candidates
tie, the quantity kind is left unresolvable and all units under it are pruned.

### Inheritance Through Quantity Kind Hierarchy

Quantity kinds form a hierarchy via `skos:broader` and `specializationOf`. If a kind
has no direct canonical unit, it inherits from the nearest ancestor with exactly one
canonical target. Ambiguous inheritance (two different targets at the same level) is
rejected.

## Pruning

Units without a viable conversion target are removed from the catalog at build time.
A unit is retained only if:
- Its multiplier is non-zero and it has a dimension vector.
- At least one of its quantity kinds resolves to a canonical unit with a matching
  dimension vector.

Non-SI-reducible units (e.g. Currency) have no canonical target and are fully pruned.

## Catalog Generation

The runtime catalog (`units.json`) is pre-generated at build time by
`backend/search/qudt/generate.go` and embedded into the binary. The generator is
not run as part of normal builds — it must be invoked manually when the QUDT data
needs updating.

### Running the Generator

```sh
cd backend/search/qudt
go run generate.go
```

This writes the updated `units.json` in the same directory. The file should then
be committed to the repository.

### Data Source

The generator downloads three RDF/Turtle vocabularies from the
[QUDT ontology](https://qudt.org/), pinned to **version 3.5.0**:

| Vocabulary        | URL                                              | Contents                            |
| ----------------- | ------------------------------------------------ | ----------------------------------- |
| Unit              | `https://qudt.org/3.5.0/vocab/unit`              | All units with conversion factors   |
| System of Units   | `https://qudt.org/3.5.0/vocab/sou`               | SI system metadata (base, coherent) |
| Quantity Kind     | `https://qudt.org/3.5.0/vocab/quantitykind`      | Quantity kind hierarchy              |

### What the Generator Does

1. Downloads and parses the three Turtle files into a single RDF graph.
2. Extracts every subject with a `conversionMultiplier` — these are the units.
3. Enriches each unit with dimension vector, offset, quantity kinds, and SI
   metadata (`hasBaseUnit`, `derivedCoherentUnitOfSystem`, `applicableSystem`,
   `scalingOf`, `siExactMatch`).
4. Collects quantity-kind parent relationships from `skos:broader` and
   `specializationOf` edges.
5. Calls `BuildCatalog()` to select canonical SI units, inherit through the
   quantity-kind hierarchy, and prune non-convertible units.
6. Writes the pruned catalog (only `multiplier`, `offset`, and `dimensionVector`
   per unit) to `units.json`.

## Runtime API

### Backend (Go, `backend/search/qudt`)

- **`Convert(value, srcUnitURI, quantityKindURI, isDelta)`** — Converts to the
  canonical SI unit for the given quantity kind. Returns `(0, false)` if the source
  is already canonical or units are unknown.
- **`ConvertTo(value, srcUnitURI, tgtUnitURI, isDelta)`** — Converts between any two
  dimensionally compatible units.
- **`Unit(unitURI)`** — Returns the `UnitInfo` (multiplier, offset, dimension vector)
  for a given unit URI.

### HTTP Endpoint

- **`POST /api/v1/quantities`** — Accepts a JSON array of
  `{unitURI, quantityKindURI, isDelta?}` objects and returns the same array enriched with a
  `conversion` field (`UnitInfo`) or `null`. A conversion is only reported when a
  dimensionally compatible canonical unit exists for the quantity kind — mirroring
  what `qudt.Convert` applied at index time; for delta quantities the offset is
  zeroed. Used by the frontend to resolve conversion factors for query-time filtering.

### Frontend (`frontend/src/unit-conversion.ts`)

- **`UnitConversionResolver`** — Groups query fields into quantity groups,
  resolves their conversions via `/quantities` (with caching), and produces a
  `QuantitySelection` mapping value fields to conversion factors.
- **`convertTermToSi(term, conversion)`** — Converts a literal filter bound to SI.
- **`statFromSi(value, conversion)`** — Converts an SI facet statistic back to the
  display unit.

## Edge Cases

- **Already-canonical values**: `Convert` returns `(0, false)` rather than the same
  value, so callers can distinguish "no conversion needed" from a converted result.
- **Unknown units**: Both `Convert` and `ConvertTo` return `(0, false)`.
- **Dimensionally incompatible units**: `ConvertTo` rejects conversion even if both
  units are known, by comparing dimension vectors.
- **Frequency vs. Activity**: Both have dimension `T-1` but different canonical units
  (Hz vs Bq). The quantity-kind lookup prevents cross-conversion between unrelated
  domains.
- **Disabled conversion**: If any required RDF predicate URI is empty, the entire
  conversion system is disabled gracefully.
- **Unresolvable frontend lookups**: A `(quantityKind, unit, isDelta)` triple without a
  conversion returns `null` from `/quantities`; the result is cached so values
  pass through unconverted instead of being re-requested. A failing request is
  not cached and degrades to unconverted filtering.
- **Ambiguous quantity kinds**: Auto-detection only applies when a quantity-kind
  facet has exactly one bucket; otherwise no kind is learned. When a kind is
  learned mid-request, the facet request is re-issued once so filters and
  statistics are computed with the same conversion state.
- **Unit facets keep original units**: Because index-time canonicalization was
  removed, identical SI values may appear under different unit URIs in unit
  facets — this is intentional so facets reflect the data as authored.
