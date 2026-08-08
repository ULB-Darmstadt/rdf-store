# Searching and filtering in SHACL_QUERY_MODE

When `SHACL_QUERY_MODE=true` (see [README.md](./README.md)), the search sidebar
replaces the generated keyword facets with a SHACL-driven query form. Instead of
free-form facets, the user picks an application profile, and the form renders one
filter control per property of that profile's SHACL shape.

This document summarizes the behavior contract of that mode: what the index must
contain, how search results are filtered, and how the query form's facet values
are computed.

## Indexing invariants

The search behavior relies on the following properties of the Solr index. They are
produced by the reindexer (`go run ./cli reindex`) and must be preserved by any
indexing change:

- Every entity that conforms to a SHACL shape becomes its own search document. The
  resource itself is a document too. Documents carry:
  - `subject` – the RDF subject of the entity.
  - `resourceId` – the owning resource, for deindexing and navigation.
  - `shape` – every shape the entity conforms to (its own shape plus inherited
    parents and alternatives). This is the entity's own conformance chain.
  - `rootShape` – the resource's root-shape chain (its root profile plus every
    profile it inherits), stamped on all documents of that resource.
- Query fields are only indexed while `SHACL_QUERY_MODE=true`. They are named
  `_query_.<hash>_<suffix>` where `<hash>` is derived from the SHACL path alone
  (see [Query field names](#query-field-names)), so the same field name is shared
  by every profile and resource that reaches the same path.
- Leaf values of nested properties are stored on the document of the entity that
  owns them and additionally mirrored onto the nearest document that conforms to
  the selected root shape. This keeps a criterion on a nested property (for
  example `owner.firstName`) matchable against a result that conforms to the
  selected profile, not only against the nested entity itself.
- Query fields are built by two traversals. The resource-root traversal above
  indexes every reachable path relative to the resource root. In addition, every
  entity is traversed from its own most specific conforming shape, so its leaf
  values are also indexed under shape-relative paths on its own document. An
  embedded entity (for example the water bath inside a cooling-process resource)
  therefore contributes to the criteria and facets of the shapes it conforms to
  itself, even though its document's `rootShape` chain is that of the containing
  resource.

## Search results: only conforming entities

The search-result list is restricted to documents that conform to the selected
profile:

```
shape:"<profileId>"
```

together with any full-text, creator, and criterion filters. Because `shape` holds
the entity's own conformance chain, this returns:

- resource documents whose root-profile chain includes the selected profile (the
  resource's root profile equals it, inherits from it, or is an alternative of it),
  and
- sub-entity documents that themselves conform to the selected profile.

It deliberately excludes documents that merely belong to a resource rooted under
the profile but do not conform to it themselves (for example the Person document
nested inside an `m4i Hardware` resource). Without a selected profile, query mode
falls back to full-text search with no conformance filter.

## Criteria (query form filters)

Each control in the query form maps to a property path of the selected shape. A
selected value produces a filter on the corresponding query field:

```
_query_.<hash(path)>_<suffix>:"<value>"
```

For nested paths the leaf value lives on the nested entity's document and on the
nearest conforming ancestor, so the criterion matches the conforming ancestor and
it appears in the result list (see [Indexing invariants](#indexing-invariants)).

## Facets: the conforming population

The value lists and counts shown in the query form are computed with the same
conformance filter as the search results:

```
shape:"<profileId>"
```

This is possible because query fields are indexed on every conforming entity's own
document (see [Indexing invariants](#indexing-invariants)): an embedded entity
carries its shape-relative values on its own document, so it contributes to the
facets of the shapes it conforms to. Non-conforming sub-entities (for example the
Person document nested inside an `m4i Hardware` resource) are excluded by both
filters, so a document only ever contributes a facet value to a profile it conforms
to, and each conforming entity is counted once.

| Purpose            | Filter                       |
|--------------------|------------------------------|
| Search results     | `shape:"<profileId>"`        |
| Facet values       | `shape:"<profileId>"`        |

## Profile chooser

The profile chooser lists the profiles that are present in the index, discovered
by faceting the `shape` field. Profiles are arranged in a parent/child tree
using the inheritance relationships from the `/profiles` backend endpoint. The
document count shown next to each profile is the number of search results you
get for it — i.e. the number of indexed documents whose `shape` chain includes
the profile (embedded entities of other resources included).

## Query field names

Query field names are derived from the SHACL path alone. The SHA-256 digest of the
path segments joined with NUL bytes is truncated to its first 16 bytes and
hex-encoded:

```
_query_.<hex(sha256(segment + "\0" + segment + ...)[0:16])>_<suffix>
```

The root shape is not part of the digest, so the same path always maps to the same
field no matter which profile or resource it is reached from; this keeps the number
of indexed fields bounded by the distinct paths rather than by the product of
shapes and resources.

The suffix follows the datatype of the leaf value:

| Suffix | Datatype / purpose                                  |
|--------|-----------------------------------------------------|
| `ds`   | numeric (`xsd:integer`, `xsd:long`, `xsd:decimal`, `xsd:double`, ...) |
| `dts`  | date / date-time (`xsd:date`, `xsd:dateTime`)       |
| `bs`   | boolean                                             |
| `ss`   | IRI values, and literals with an unmapped datatype (which are stored as both `ss` and `txt`) |
| `txt`  | `contains` operator / full-text within a value      |
| `srpt` | spatial values (configured geo datatype, WKT)       |

The frontend computes the same names when resolving form controls against the
indexed field list: it digests `path.join('\0')`, so both sides must keep the
NUL-separated (not NUL-prefixed) form in sync.

## Operational notes

- After changing the indexer or shape configuration, rebuild the index:
  `go run ./cli reindex` (from `backend/`, with the environment loaded). Query
  fields for newly available profiles only appear after a reindex.
- Query fields are skipped entirely when `SHACL_QUERY_MODE=false` to keep the
  indexed field count small.
