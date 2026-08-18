import { DataFactory, Literal } from 'n3'
import { BACKEND_URL } from './constants'

// init languages based on browser languages
const languages = [...new Set(navigator.languages.flatMap(lang => {
    if (lang.length > 2) {
        // for each 5 letter lang code (e.g. de-DE) append its corresponding 2 letter code (e.g. de) directly afterwards
        return [lang.toLocaleLowerCase(), lang.substring(0, 2)]
    }
    return lang
})), ''] // <-- append empty string to accept RDF literals with no language

const appLabels: Record<string, Literal[]> = {
    'fulltextsearch': [DataFactory.literal('Full-text search', 'en'), DataFactory.literal('Volltextsuche', 'de')],
    'fulltextsearchplaceholder': [DataFactory.literal('Search query...', 'en'), DataFactory.literal('Suchanfrage...', 'de')],
    'shape': [DataFactory.literal('Profile', 'en'), DataFactory.literal('Profil', 'de')],
    'selectprofile': [DataFactory.literal('Select profile', 'en'), DataFactory.literal('Profil auswählen', 'de')],
    'selectedprofile': [DataFactory.literal('Selected profile', 'en'), DataFactory.literal('Ausgewähltes Profil', 'de')],
    'selectprofile-hint': [DataFactory.literal('Select a profile to show search filters', 'en'), DataFactory.literal('Profil auswählen, um Suchfilter anzuzeigen', 'de')],
    'add_resource': [DataFactory.literal('Add resource', 'en'), DataFactory.literal('Ressource hinzufügen', 'de')],
    'cancel': [DataFactory.literal('Cancel', 'en'), DataFactory.literal('Abbrechen', 'de')],
    'save': [DataFactory.literal('Save', 'en'), DataFactory.literal('Speichern', 'de')],
    'delete': [DataFactory.literal('Delete', 'en'), DataFactory.literal('Löschen', 'de')],
    'edit': [DataFactory.literal('Edit', 'en'), DataFactory.literal('Bearbeiten', 'de')],
    'export': [DataFactory.literal('Export', 'en'), DataFactory.literal('Exportieren', 'de')],
    'export_graph': [DataFactory.literal('Export graph', 'en'), DataFactory.literal('Graph exportieren', 'de')],
    'new': [DataFactory.literal('New', 'en'), DataFactory.literal('Neu:', 'de')],
    'results': [DataFactory.literal('Results', 'en'), DataFactory.literal('Ergebnisse', 'de')],
    'noresults': [DataFactory.literal('No results', 'en'), DataFactory.literal('Keine Ergebnisse', 'de')],
    'of': [DataFactory.literal('of', 'en'), DataFactory.literal('von', 'de')],
    'pages': [DataFactory.literal('Pages', 'en'), DataFactory.literal('Seiten', 'de')],
    'loading': [DataFactory.literal('Loading', 'en'), DataFactory.literal('Lade', 'de')],
    'loading_filters': [DataFactory.literal('Loading filters...', 'en'), DataFactory.literal('Lade Filter...', 'de')],
    'no_filters': [DataFactory.literal('No filters available.', 'en'), DataFactory.literal('Keine Filter verfügbar.', 'de')],
    'sign_in': [DataFactory.literal('Sign in', 'en'), DataFactory.literal('Anmelden', 'de')],
    'sign_out': [DataFactory.literal('Sign out', 'en'), DataFactory.literal('Abmelden:', 'de')],
    'no_label': [DataFactory.literal('<No label>', 'en'), DataFactory.literal('<Kein Label>', 'de')],
    'error': [DataFactory.literal('Error', 'en'), DataFactory.literal('Fehler', 'de')],
    'resource_save_failed': [DataFactory.literal('Failed saving resource', 'en'), DataFactory.literal('Ressource konnte nicht gespeichert werden', 'de')],
    'resource_save_succeeded': [DataFactory.literal('Resource saved', 'en'), DataFactory.literal('Ressource gespeichert', 'de')],
    'resource_delete_failed': [DataFactory.literal('Failed deleting resource', 'en'), DataFactory.literal('Ressource konnte nicht gelöscht werden', 'de')],
    'resource_delete_succeeded': [DataFactory.literal('Resource deleted', 'en'), DataFactory.literal('Ressource gelöscht', 'de')],
    'resource_delete_confirmation': [DataFactory.literal('Do you really want to delete this resource?', 'en'), DataFactory.literal('Möchten Sie diese Ressource wirklich löschen?', 'de')],
    'search_filter_own': [DataFactory.literal('Only own resources', 'en'), DataFactory.literal('Nur eigene Ressourcen', 'de')],
    'click_hit_to_view': [DataFactory.literal('Click on a search result to display here', 'en'), DataFactory.literal('Suchergebnis zur Anzeige auswählen', 'de')],
    'graph_view': [DataFactory.literal('Graph view', 'en'), DataFactory.literal('Graphanzeige', 'de')],
    'graph_loading': [DataFactory.literal('Loading graph…', 'en'), DataFactory.literal('Graph wird geladen…', 'de')],
    'graph_load_failed': [DataFactory.literal('Could not load graph', 'en'), DataFactory.literal('Graph konnte nicht geladen werden', 'de')],
    'graph_load_incoming': [DataFactory.literal('Incoming links', 'en'), DataFactory.literal('Eingehende Links', 'de')],
    'graph_load_outgoing': [DataFactory.literal('Outgoing links', 'en'), DataFactory.literal('Ausgehende Links', 'de')],
    'graph_load_more': [DataFactory.literal('Load more', 'en'), DataFactory.literal('Mehr laden', 'de')],
    'graph_incoming_loaded': [DataFactory.literal('All incoming loaded', 'en'), DataFactory.literal('Alle eingehenden geladen', 'de')],
    'graph_outgoing_loaded': [DataFactory.literal('All outgoing loaded', 'en'), DataFactory.literal('Alle ausgehenden geladen', 'de')],
    'graph_focus': [DataFactory.literal('Focus here', 'en'), DataFactory.literal('Hier fokussieren', 'de')],
    'graph_fit': [DataFactory.literal('Fit graph', 'en'), DataFactory.literal('Graph einpassen', 'de')],
    'graph_automatic_limited': [DataFactory.literal('Showing a bounded connected subgraph (up to 50 nodes or 100 links). Select a node to load another page, or focus it to start a fresh graph.', 'en'), DataFactory.literal('Es wird ein begrenzter verbundener Teilgraph angezeigt (bis zu 50 Knoten oder 100 Links). Wählen Sie einen Knoten, um eine weitere Seite zu laden, oder fokussieren Sie ihn für einen neuen Graphen.', 'de')],
    'graph_reset': [DataFactory.literal('Reset to search result', 'en'), DataFactory.literal('Zum Suchergebnis zurücksetzen', 'de')],
    'graph_nodes': [DataFactory.literal('nodes', 'en'), DataFactory.literal('Knoten', 'de')],
    'graph_edges': [DataFactory.literal('links', 'en'), DataFactory.literal('Links', 'de')],
    'graph_actions_for': [DataFactory.literal('Graph actions for', 'en'), DataFactory.literal('Graphaktionen für', 'de')],
    'detail_view': [DataFactory.literal('Detail view', 'en'), DataFactory.literal('Detailanzeige', 'de')],
    'last_modified': [DataFactory.literal('Last modified', 'en'), DataFactory.literal('Zuletzt geändert', 'de')],
    'api_documentation': [DataFactory.literal('API documentation', 'en'), DataFactory.literal('API Dokumentation', 'de')],
    'imprint': [DataFactory.literal('Imprint', 'en'), DataFactory.literal('Impressum', 'de')],
    'privacy': [DataFactory.literal('Privacy Statement', 'en'), DataFactory.literal('Datenschutzerklärung', 'de')]
}

export function registerLabel(key: string, literals: Literal[]) {
    i18n[key] = findBestMatchingLiteral(literals)
}

export async function fetchLabels(ids: string[], surroundWithBrackets = false, prependColon = false) {
    const formData = new URLSearchParams()
    const transormed: Record<string, string> = {}
    for (const id of ids) {
        // load id only if not already available or requested before
        if (i18n[id] === undefined) {
            let transformedId = id
            if (prependColon) {
                transformedId = ':' + transformedId
            }
            if (surroundWithBrackets) {
                transformedId = '<' + transformedId + '>'
            }
            formData.append('id', transformedId)
            transormed[transformedId] = id
        }
    }
    if (formData.size > 0) {
        formData.append('lang', navigator.language)
        const resp: Record<string, string> = await fetch(`${BACKEND_URL}/labels`, { method: 'POST', body: formData }).then(r => r.json())
        // iterate only over actually requested ids
        formData.forEach((v, k) => {
            if (k === 'id') {
                i18n[transormed[v]] = resp[v] || ''
            }
        })
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
