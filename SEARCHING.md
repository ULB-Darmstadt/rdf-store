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

Every indexed SHACL value is a nested child with `docType:value`, a stable `path`
identifier, and fixed typed fields:

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
`datatype` and `language` are stored when present. Child IDs are deterministic,
and repeated path/value pairs are deduplicated per parent.

Nested leaf values are stored on their owning entity and mirrored to the nearest
ancestor entity that conforms to the traversal's selected shape. This lets a
dataset criterion such as `location.label` return the dataset while a query for
the location profile can still return the location entity itself.

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

## Rebuilding

The architecture intentionally has no migration compatibility with the former
dynamic `_query_.*` fields. Rebuild the collection after deploying it:

```bash
cd backend
go run ./cli reindex
```

Reindexing deletes and recreates the Solr collection before loading RDF resources.
