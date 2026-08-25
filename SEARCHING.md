# Searching and filtering in SHACL query mode

The search sidebar uses the selected application profile to render a SHACL query
form. Solr uses a fixed schema: SHACL paths are
values in nested documents, never Solr field names.

## Index layout

Each conforming RDF entity is one parent document:

```json
{
  "id": "<resource>|<subject>",
  "docType": "entity",
  "resourceId": "<resource>",
  "subject": "<subject>",
  "shape": ["<own shape>", "<inherited shape>"],
  "_childDocuments_": []
}
```

`shape` contains only shapes to which that subject conforms. Shapes of referenced
entities are not copied to the parent.

Every indexed SHACL path is a nested child with `docType:value`, a stable `path`
identifier, and fixed multivalued typed fields:

| Value | Solr field |
|---|---|
| exact literal or IRI | `valueString` |
| analyzed text | `valueText` |
| number | `valueNumber` |
| date or date-time | `valueDate` |
| boolean | `valueBoolean` |
| WKT geometry | `valueGeo` |

Text literals populate both `valueString` and `valueText`, supporting exact
facets and `contains` queries without creating additional physical fields.
`datatype` and `language` are stored when present. Values sharing a path are
grouped in one child document, whose ID is deterministic for the parent and path.
Repeated values are deduplicated within each typed field.

Nested leaf values are stored on their owning entity and mirrored to the nearest
ancestor entity that conforms to the traversal's selected shape. This lets a
dataset criterion such as `location.label` return the dataset while a query for
the location profile can still return the location entity itself.

## Linked resources

Indexing expands links to resources stored in the local RDF dataset recursively.
Consequently, a SHACL path may cross named-graph boundaries: if a dataset links
to a separately stored location, for example, `location.label` is indexed from
the location graph onto the dataset document. Cycles are detected, and graph and
local-subject lookups are cached for the duration of an indexing operation.

The expansion affects query values, not result ownership. Only subjects from the
resource's own graph that conform to a shape produce `docType:entity` documents.
Linked subjects supply nested `docType:value` documents but do not become extra
entity documents owned by every referring resource. Metadata therefore keeps two
conformance views:

- `dcterms:conformsTo` records conformance for subjects owned by the resource and
  determines which entity documents exist.
- `urn:rdf-store:queryConformsTo` also records conformance for linked subjects in
  this resource's validation context and guides traversal of linked query paths.

The second view is computed when resource metadata is created or rebuilt. Normal
indexing consumes the persisted result and does not run SHACL validation.

Because every unique parent/path combination is a nested Solr document, including
linked paths can increase Solr's raw document count. Grouping all values for the
same path avoids a separate document for every RDF value. The remaining growth
does not increase the number of search results: result queries and profile facets
count only `docType:entity`, while value facets use `uniqueBlock(_root_)` to count
distinct parent entities.

## Path identity

The path identifier is the first 16 bytes of SHA-256 over the expanded SHACL path
segments separated by NUL bytes, encoded as 32 lowercase hex characters:

```text
hex(sha256(segment + "\0" + segment + ...)[0:16])
```

Qualified SHACL property identifiers are used as path segments where necessary.
The frontend computes the same identifier; it does not inspect Solr's field list.

## Search filters

Normal result searches always include `docType:entity`, so value children never
appear as hits. Selecting a profile adds:

```text
shape:"<profile-id>"
```

Each criterion is a separate block-join parent filter. For example, a numeric
range is translated conceptually to:

```text
{!parent which=docType:entity}(
  docType:value AND path:"<path-id>" AND valueNumber:[10 TO 20]
)
```

Separate joins are essential: two criteria may be satisfied by two different
value children. Full-text, creator, sorting, and pagination remain parent
operations.

## Facets

Facet requests first restrict the entity-parent population with the same profile
and active-criterion filters used by search. A `blockChildren` domain then selects
value children by `path` and aggregates the appropriate typed field. Bucket and
availability counts use `uniqueBlock(_root_)`, so they count distinct entity
parents rather than raw child documents.

The profile chooser facets `shape` only on `docType:entity`; therefore its counts
match the number of searchable conforming entities.

## Keeping linked values current

Updating a resource can change the search documents of resources that refer to
it. The update path finds direct and transitive referrers both before and after
the graph replacement, rebuilds their contextual conformance metadata, and
reindexes the complete affected set. Looking at both link states also covers
links added or removed by the update.

Resource updates are serialized so two dependency refreshes cannot overwrite
each other. Before changing data, the service snapshots the updated resource and
the affected metadata. If conformance rebuilding or Solr indexing fails, it
restores the RDF graph, metadata, and affected search documents.

Deletion remains forbidden while another resource graph links to the resource
or one of its subjects. The API returns a conflict instead of leaving referring
search documents with stale linked values.

## Rebuilding

The architecture intentionally has no migration compatibility with the former
dynamic `_query_.*` fields or with metadata that predates linked query
conformance. Rebuild metadata and the collection after deploying it:

```bash
cd backend
go run ./cli rebuild
```

`rebuild` validates all stored resources, persists both conformance views, and
then performs one full reindex. A plain `reindex` reuses persisted conformance;
if a linked resource lacks `queryConformsTo` metadata, it stops before deleting
the existing Solr collection and asks for a rebuild.

Full reindexing prepares all documents before replacing the collection, reuses a
shared linked-resource cache, submits parent document blocks in batches of 100,
and commits only the final batch. This avoids repeated linked-graph queries and
per-resource Solr commits. Profile synchronization likewise rebuilds all resource
metadata once and follows it with one batched reindex, because profile changes
can alter contextual conformance throughout the link graph.
