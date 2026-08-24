# Changelog

This project is deployed continuously by pulling the `main` branch and does not
currently publish versioned releases. The **Unreleased** section describes
changes on `main` that have not yet been recorded in a dated checkpoint. Git
history remains the authoritative record of exactly what was deployed.

Historical entries below were reconstructed from commit history. They summarize
significant changes rather than documenting every commit.

## Unreleased

### Changed

- Keep quantity-unit facet choices stable while other query criteria change.
- Clear a related quantity range when its selected unit changes, preventing a
  range entered for one unit from being interpreted as another.
- Update `@ulb-darmstadt/shacl-form` to 3.5.0 and `@ro-kit/ui-widgets` to 1.0.61.

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
