# Changelog

This project is deployed continuously by pulling the `main` branch and does not
currently publish versioned releases. The **Unreleased** section describes
changes on `main` that have not yet been recorded in a dated checkpoint. Git
history remains the authoritative record of exactly what was deployed.

Historical entries below were reconstructed from commit history. They summarize
significant changes rather than documenting every commit.

## Unreleased

### Added

- Add user API keys for accounts with write access. Keys are generated through
  `POST /api-key`, are accepted via the `X-API-Key` header, and restore the
  owning user's identity, email, and groups for authorization. Generating a new
  key revokes the previous key for that user; API keys cannot be used to
  generate or renew keys.
- Store only SHA-256 hashes of API keys in Redis at `API_KEY_REDIS_URL`, with a
  lifetime configured by `API_KEY_TTL` (default `168h`). Keys are disabled when
  authentication or the Redis URL is not configured; docker-compose supplies the
  Redis URL by default. Frontend login silently renews the active key.

### Changed

- Expose `apiKeyEnabled` in `/config` and allow `X-API-Key` in CORS requests.
- Define JWT, API key, and optional OpenID Connect security schemes in the
  OpenAPI specification.

## 2026-11

### Fixed

- Match text filters against complete literal values in `valueString`, preserving
  spaces, punctuation, and word order without case sensitivity. Multi-word
  affiliation names such as "Technical University of Darmstadt" now return
  matching people, and search input is treated literally rather than as regex
  syntax.

## 2026-10

### Added

- Make the graph view's automatic loading limits configurable via the
  `GRAPH_NODE_LIMIT` and `GRAPH_EDGE_LIMIT` environment variables
  (defaults: 50 nodes, 100 links), exposed to the frontend through `/config`.
- Add clickable per-node badges for expanding incoming and outgoing neighbors
  within those limits, with a notice when the bounded subgraph reaches capacity.

### Changed

- Render the graph with the classic force-directed layout and simple arc
  links again, replacing the radial node action menu; newly revealed nodes are
  animated. The short-lived `GRAPH_LOAD_MORE` option was removed, so manual
  expansion is always available.
- Update `@ulb-darmstadt/shacl-form` to 3.7.4.

## 2026-09

### Added

- Return each quantity kind's canonical SI unit from `/quantities`, including
  kind-only requests that do not yet have a selected source unit.
- Support recursive RDF collections throughout profile detection, validation,
  full-text indexing, and query-facet indexing.

### Changed

- Pre-select canonical SI units in available quantity facets, injecting the
  canonical unit when it is absent from Solr buckets and using quantity kinds
  or convertible units fixed by `sh:hasValue`. Apply a shared canonical result
  to every matching facet while retaining a single batched `/quantities` call.
- Keep quantity-unit facet choices stable while other query criteria change.
- Clear a related quantity range when its selected unit changes, preventing a
  range entered for one unit from being interpreted as another.
- Include recursively linked local resources when indexing nested query paths,
  so filters reflect values stored in linked RDF graphs.
- Reindex direct and transitive referring resources when a linked resource is
  updated, preventing denormalized linked fields from becoming stale; failed
  dependent refreshes restore the prior RDF, metadata, and search documents.
- Cache shared linked-graph lookups and submit rebuilt search documents in
  batches with a single final commit, substantially reducing full-reindex time.
- Group all values for the same entity and SHACL path into one multivalued Solr
  child document, reducing nested-document overhead without changing filters or
  facets.
- Persist contextual linked-resource conformance during metadata validation so
  subsequent full reindexes do not repeat SHACL validation.
- Abort a reindex before replacing the Solr collection when resource metadata
  has not yet been migrated with `rdf-store-cli rebuild`.
- Rebuild all resource metadata and perform one batched reindex after profile
  changes, keeping contextual linked conformance synchronized.
- Return nonzero CLI exit codes when metadata rebuilding or reindexing fails.
- Treat `rdf:first` and `rdf:rest` arcs as collection structure rather than
  linked-resource references.
- Show anonymous literal-only RDF collections as repeated values in the owning
  resource's graph information pane, while retaining named, resource-valued,
  mixed, and annotated collections as graph nodes.
- Update `@ulb-darmstadt/shacl-form` to 3.7.1 and resolve
  `@ro-kit/ui-widgets` 1.0.63.

## 2026-08

### Added

- Added canonical QUDT unit conversion during indexing and quantity-aware unit
  conversion when filtering.
- Expanded graph navigation and introduced additional graph layout strategies.

### Changed

- Reworked indexing and faceting, including improved label extraction and
  support for `dash:facet`.
- Redesigned query-form loading, profile selection, and resource-creation flows.
- Improved handling of delta quantities and local resource imports.

## 2026-07

### Added

- Integrated the SHACL form query mode and a profile chooser for filtering.
- Added support for arbitrary RDF subject identifiers.
- Added confirmation before resource deletion and allowed full-text search
  without selecting a profile.

### Changed

- Improved qualified-shape-path queries, inherited-profile filtering, and query
  loading feedback.
- Overhauled the application layout and scrolling behavior.

## 2026-05 to 2026-06

### Changed

- Indexed all numeric values as doubles and expanded property handling for
  `sh:or` and `sh:xone`.
- Improved resource metadata management, including creation timestamps.
- Added build caching and updated core frontend and backend dependencies.
- Added profile IRI tooltips.

## 2026-03 to 2026-04

### Added

- Added configurable label prefixes for facets targeting qualified value
  shapes.
- Added footer links for imprint and privacy information.
- Added facet support for node shapes using `sh:class`.

### Changed

- Upgraded Solr to version 10.
- Improved OAuth configuration documentation and fixed profile-facet ordering
  in Chrome.

## 2026-02

### Changed

- Improved indexing, profile filtering, label extraction, and recursive link
  resolution.
- Preferred local profiles over equivalent remote profiles.
- Added resource-link provider support and refined layout and localization.

## 2026-01

### Added

- Added command-line synchronization and index rebuild commands.
- Added OpenAPI documentation, service health checks, and endpoints for querying
  resources by SHACL conformance.
- Added stored resource-conformance metadata and deeper validation support.

### Changed

- Hardened the RDF proxy, graph view, request validation, origin handling, and
  Solr query escaping.
- Improved incremental reindexing when profiles change and prevented deletion
  of resources referenced by other resources.
- Improved linked-resource loading, profile selection, indexing, paging, and
  error handling.
- Fixed startup races that could recreate the search index unintentionally.

## 2025-12

### Added

- Added the RDF graph viewer, client-side routing, localization, and initial
  Swagger/OpenAPI support.
- Added standard taxonomy loading and label extraction for the Solr index.

### Changed

- Improved graph layout and information panels, OAuth redirects, and SHACL
  validation result handling.

## 2025-11

### Added

- Created the RDF store application and its initial installation setup.
- Added caching for parsed validator data, fetched profiles, and failed remote
  responses.
