import { DataFactory, Literal } from "n3"
import { BACKEND_URL } from "./constants"

// init languages based on browser languages
const languages = [...new Set(navigator.languages.flatMap(lang => {
    if (lang.length > 2) {
        // for each 5 letter lang code (e.g. de-DE) append its corresponding 2 letter code (e.g. de) directly afterwards
        return [lang.toLocaleLowerCase(), lang.substring(0, 2)]
    } 
    return lang
})), ''] // <-- append empty string to accept RDF literals with no language

const appLabels: Record<string, Literal[]> = {
    'fulltextsearch' : [ DataFactory.literal('Full-text search', 'en'), DataFactory.literal('Volltextsuche', 'de') ],
    'fulltextsearchplaceholder' : [ DataFactory.literal('Search query...', 'en'), DataFactory.literal('Suchanfrage...', 'de') ],
    '_shape' : [ DataFactory.literal('Profile', 'en'), DataFactory.literal('Profil', 'de') ],
    'selectprofile' : [ DataFactory.literal('Select profile', 'en'), DataFactory.literal('Profil auswählen', 'de') ],
    'add_resource' : [ DataFactory.literal('Add resource', 'en'), DataFactory.literal('Ressource hinzufügen', 'de') ],
    'save' : [ DataFactory.literal('Save', 'en'), DataFactory.literal('Speichern', 'de') ],
    'delete' : [ DataFactory.literal('Delete', 'en'), DataFactory.literal('Löschen', 'de') ],
    'new' : [ DataFactory.literal('New', 'en'), DataFactory.literal('Neu:', 'de') ],
    'results' : [ DataFactory.literal('Results', 'en'), DataFactory.literal('Ergebnisse', 'de') ],
    'noresults' : [ DataFactory.literal('No results', 'en'), DataFactory.literal('Keine Ergebnisse', 'de') ],
    'of' : [ DataFactory.literal('of', 'en'), DataFactory.literal('von', 'de') ],
    'pages' : [ DataFactory.literal('Pages', 'en'), DataFactory.literal('Seiten', 'de') ],
    'loading' : [ DataFactory.literal('Loading', 'en'), DataFactory.literal('Lade', 'de') ],
    'sign_in' : [ DataFactory.literal('Sign in', 'en'), DataFactory.literal('Anmelden', 'de') ],
    'sign_out' : [ DataFactory.literal('Sign out', 'en'), DataFactory.literal('Abmelden:', 'de') ],
    'error' : [ DataFactory.literal('Error', 'en'), DataFactory.literal('Fehler', 'de') ],
    'resource_save_failed' : [ DataFactory.literal('Failed saving resource', 'en'), DataFactory.literal('Ressource konnte nicht gespeichert werden', 'de') ],
    'resource_save_succeeded' : [ DataFactory.literal('Resource saved', 'en'), DataFactory.literal('Ressource gespeichert', 'de') ],
    'resource_delete_failed' : [ DataFactory.literal('Failed deleting resource', 'en'), DataFactory.literal('Ressource konnte nicht gelöscht werden', 'de') ],
    'resource_delete_succeeded' : [ DataFactory.literal('Resource deleted', 'en'), DataFactory.literal('Ressource gelöscht', 'de') ],
    'search_filter_own' : [ DataFactory.literal('Only own resources', 'en'), DataFactory.literal('Nur eigene Ressourcen', 'de') ],
}

export async function fetchLabels(ids: string[], surroundWithBrackets = false, prependColon = false) {
    let atLeastOneIdMissing = false
    const formData = new URLSearchParams()
    formData.append('lang', navigator.language)
    for (let id of ids) {
        // load id only if not already available or requested before
        if (i18n[id] === undefined) {
            atLeastOneIdMissing = true
            if (prependColon) {
                id = ':' + id
            }
            if (surroundWithBrackets) {
                id = '<' + id + '>'
            }
            formData.append('id', id)
        }
    }
    if (atLeastOneIdMissing) {
        const resp = await fetch(`${BACKEND_URL}/labels`, { method: "POST", body: formData })
        const labelsBatch: Record<string, string> = await resp.json()
        for (let [id, label] of Object.entries(labelsBatch)) {
            // clean up leading '<' and trailing '>'
            if (surroundWithBrackets && id.startsWith('<') && id.endsWith('>')) {
                id = id.substring(1, id.length - 1)
            }
            if (prependColon) {
                id = id.substring(1, id.length)
            }
            // put empty string in i18n[] if server has no label to prevent requesting it again
            label = label || i18n[id] || ''
            i18n[id] = label
        }
    }
}

export function findBestMatchingLiteral(literals: Literal[]): string {
    let candidate: Literal | undefined
    for (const literal of literals) {
        candidate = prioritizeByLanguage(candidate, literal)
    }
    return candidate ? candidate.value : ''
}

function prioritizeByLanguage(text1?: Literal, text2?: Literal): Literal | undefined {
    if (text1 === undefined) {
        return text2
    }
    if (text2 === undefined) {
        return text1
    }
    const index1 = languages.indexOf(text1.language)
    if (index1 < 0) {
        return text2
    }
    const index2 = languages.indexOf(text2.language)
    if (index2 < 0) {
        return text1
    }
    return index2 > index1 ? text1 : text2
}

// init static UI labels
export const i18n: Record<string, string> = {}
for (const key in appLabels) {
    i18n[key] = findBestMatchingLiteral(appLabels[key])
}
