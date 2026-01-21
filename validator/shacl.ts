import { Store } from 'n3'
import { Term } from '@rdfjs/types'
import { shapesGraphName } from './validator'
const prefixSHACL = 'http://www.w3.org/ns/shacl#'
const shaclNode = prefixSHACL + 'node'
const shaclProperty = prefixSHACL + 'property'
const shaclAnd = prefixSHACL + 'and'

export function getExtendedShapes(subject: Term, dataset: Store) {
    const extendedShapes: Term[] = []
    for (const shape of dataset.getObjects(subject, shaclNode, shapesGraphName)) {
        extendedShapes.push(shape)
    }
    const andLists = dataset.getQuads(subject, shaclAnd, null, shapesGraphName)
    if (andLists.length > 0) {
        const lists = dataset.extractLists()
        for (const andList of andLists) {
            for (const shape of lists[andList.object.value]) {
                extendedShapes.push(shape)
            }
        }
    }
    for (const shape of extendedShapes) {
        // recurse up
        extendedShapes.push(...getExtendedShapes(shape, dataset))
    }
    return extendedShapes
}