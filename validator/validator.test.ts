import assert from 'node:assert/strict'
import test from 'node:test'
import { validate } from './validator.ts'

const shapes = `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix rdfs: <http://www.w3.org/2000/01/rdf-schema#> .
@prefix ex: <http://example.org/> .

# Imported vocabularies may use a named list head as a documented resource.
# It is not a SHACL list and must not prevent valid shape lists from loading.
ex:DocumentedList rdf:first ex:item ; rdf:rest rdf:nil ; rdfs:label "List" .

ex:Root a sh:NodeShape ;
  sh:and ( ex:RequiredA ex:RequiredB ) ;
  sh:property [
    sh:path ex:child ;
    sh:or ( [ sh:node ex:TypeA ] [ sh:node ex:TypeB ] )
  ] ;
  sh:property [
    sh:path ex:qualified ;
    sh:qualifiedValueShape ex:Qualified ;
    sh:qualifiedMinCount 1
  ] .
ex:RequiredA a sh:NodeShape ;
  sh:property [ sh:path ex:a ; sh:minCount 1 ] .
ex:RequiredB a sh:NodeShape ;
  sh:property [ sh:path ex:b ; sh:minCount 1 ] .
ex:TypeA a sh:NodeShape ;
  sh:property [ sh:path ex:alpha ; sh:minCount 1 ] .
ex:TypeB a sh:NodeShape ;
  sh:property [ sh:path ex:beta ; sh:minCount 1 ] .
ex:Qualified a sh:NodeShape ;
  sh:property [ sh:path ex:score ; sh:datatype xsd:decimal ] .
`

const data = `
@prefix ex: <http://example.org/> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .
ex:resource ex:a "a" ; ex:b "b" ; ex:child ex:child1 ; ex:qualified ex:q1 .
ex:child1 ex:alpha "present" .
ex:q1 ex:score "12.5"^^xsd:decimal .
`

test('retains all conjunctive and only conforming alternative shapes', async () => {
    const result = await validate(shapes, 'http://example.org/Root', data, 'http://example.org/resource')
    assert.deepEqual(new Set(result['http://example.org/resource']), new Set([
        'http://example.org/Root',
        'http://example.org/RequiredA',
        'http://example.org/RequiredB',
    ]))
    assert.ok(result['http://example.org/child1'].includes('http://example.org/TypeA'))
    assert.ok(!result['http://example.org/child1'].includes('http://example.org/TypeB'))
    assert.equal(result['http://example.org/child1'].filter(shape => shape.startsWith('n3-')).length, 1)
    assert.deepEqual(result['http://example.org/q1'], ['http://example.org/Qualified'])
})

test('does not record shapes that fail validation', async () => {
    const incompleteData = `
@prefix ex: <http://example.org/> .
ex:resource ex:a "a" ; ex:child ex:child1 ; ex:qualified ex:q1 .
ex:child1 ex:alpha "present" .
`
    const result = await validate(shapes, 'http://example.org/Root', incompleteData, 'http://example.org/resource')
    assert.deepEqual(result, {})
})

test('records only the matching qualified sibling shape for each value', async () => {
    const qualifiedSiblingShapes = `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix ex: <http://example.org/> .

ex:Root a sh:NodeShape ;
  sh:property [
    sh:path ex:measurement ;
    sh:qualifiedValueShape ex:Temperature
  ] ;
  sh:property [
    sh:path ex:measurement ;
    sh:qualifiedValueShape ex:Time
  ] .
ex:Temperature a sh:NodeShape ;
  sh:property [ sh:path ex:quantityKind ; sh:hasValue ex:TemperatureKind ] .
ex:Time a sh:NodeShape ;
  sh:property [ sh:path ex:quantityKind ; sh:hasValue ex:TimeKind ] .
`
    const qualifiedSiblingData = `
@prefix ex: <http://example.org/> .
ex:resource ex:measurement ex:temperature, ex:time .
ex:temperature ex:quantityKind ex:TemperatureKind .
ex:time ex:quantityKind ex:TimeKind .
`
    const result = await validate(
        qualifiedSiblingShapes,
        'http://example.org/Root',
        qualifiedSiblingData,
        'http://example.org/resource',
    )
    assert.deepEqual(result['http://example.org/temperature'], ['http://example.org/Temperature'])
    assert.deepEqual(result['http://example.org/time'], ['http://example.org/Time'])
})

test('validates RDF collection resources without false violations', async () => {
    const listShapes = `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .
@prefix xsd: <http://www.w3.org/2001/XMLSchema#> .

ex:VariableShape a sh:NodeShape ;
  sh:property [
    sh:path ex:hasDiscreteValues ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:node ex:DecimalListShape
  ] .

ex:DecimalListShape a sh:NodeShape ;
  sh:property [
    sh:path rdf:first ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:datatype xsd:decimal
  ] ;
  sh:property [
    sh:path rdf:rest ;
    sh:minCount 1 ;
    sh:maxCount 1 ;
    sh:or (
      [ sh:hasValue rdf:nil ]
      [ sh:node ex:DecimalListShape ]
    )
  ] .
`
    const listData = `
@prefix ex: <http://example.org/> .
<http://example.org/resource> ex:hasDiscreteValues ( 1.5 2.75 4.0 ) .
`
    const result = await validate(
        listShapes,
        'http://example.org/VariableShape',
        listData,
        'http://example.org/resource',
    )
    assert.ok(result['http://example.org/resource'].includes('http://example.org/VariableShape'))
})

test('records conformance for node-shaped RDF collection members', async () => {
    const listShapes = `
@prefix sh: <http://www.w3.org/ns/shacl#> .
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix ex: <http://example.org/> .
ex:Root a sh:NodeShape ; sh:property [
  sh:path ex:pets ; sh:maxCount 1 ; sh:node ex:PetList
] .
ex:PetList a sh:NodeShape ;
  sh:property [ sh:path rdf:first ; sh:minCount 1 ; sh:maxCount 1 ; sh:node ex:Pet ] ;
  sh:property [ sh:path rdf:rest ; sh:minCount 1 ; sh:maxCount 1 ;
    sh:or ( [ sh:hasValue rdf:nil ] [ sh:node ex:PetList ] )
  ] .
ex:Pet a sh:NodeShape ; sh:property [ sh:path ex:name ; sh:minCount 1 ] .
`
    const listData = `
@prefix ex: <http://example.org/> .
ex:resource ex:pets ( ex:fido ) .
ex:fido ex:name "Fido" .
`
    const result = await validate(listShapes, 'http://example.org/Root', listData, 'http://example.org/resource')
    assert.ok(result['http://example.org/fido'].includes('http://example.org/Pet'))
})
