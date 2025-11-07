import { html, css, unsafeCSS, nothing } from 'lit'
import { customElement, query } from 'lit/decorators.js'
import leafletCss from 'leaflet/dist/leaflet.css?inline'
import leafletFullscreenCss from 'leaflet.fullscreen/Control.FullScreen.css?inline'
import 'leaflet.fullscreen/Control.FullScreen.js'
import 'leaflet.heat/dist/leaflet-heat.js'
import * as L from 'leaflet'

import { Facet } from './base'
import { AggregationFacet, QueryFacet } from '../solr'

var worldBounds: L.LatLngBounds = L.latLngBounds({ lng: -180, lat: -90}, { lng: 180, lat: 90 })

@customElement('geolocation-facet')
export class GeoLocationFacet extends Facet {
    static styles = [...Facet.styles, css`
        #label { padding: 0.35em 0; }
        #map { height: 250px; width: 100%; }
        #map .leaflet-pane, #map .leaflet-top { z-index: 0; }
        #map .leaflet-heatmap-layer { opacity: 0.5; }
    `, unsafeCSS(leafletCss), unsafeCSS(leafletFullscreenCss)]

    @query('#map')
    container!: HTMLElement
    map!: L.Map
    // @ts-ignoref
    heatLayer = L.heatLayer([])
    mapBounds = worldBounds

    constructor(indexField: string) {
        super(indexField)
    }

    firstUpdated() {
        setTimeout(() => {
            this.map = L.map(this.container, {
                attributionControl: false,
                fullscreenControl: true,
                maxBoundsViscosity: 1,
                zoom: 0,
                layers: [
                    L.tileLayer('https://tile.openstreetmap.de/{z}/{x}/{y}.png'),
                    // L.tileLayer('https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}'),
                    this.heatLayer,
                ]
            })
            this.map.fitBounds(worldBounds).setMaxBounds(worldBounds)
            this.map.on('moveend', () => {
                this.mapBounds = this.map.getBounds()
                this.active = this.map.getZoom() > 0
                this.dispatchEvent(new Event('change', { bubbles: true }))
            })
        })
    }

    updateValues(aggs: Record<string, AggregationFacet>): void {
        const heatmap = aggs[this.indexField]?.counts_ints2D?.length ? aggs[this.indexField] : undefined
        const heatData = []
        if (heatmap?.counts_ints2D) {
            const minLng = heatmap.minX!
            const maxLat = heatmap.maxY!
            const dLng = (heatmap.maxX! - minLng) / heatmap.columns!
            const dLat = (heatmap.maxY! - heatmap.minY!) / heatmap.rows!
            
            for (let y = 0; y < heatmap.counts_ints2D.length; y++) {
                const row = heatmap.counts_ints2D[y]
                if (row !== null) {
                    for (let x = 0; x < row.length; x++) {
                        if (row[x] > 0) {
                            heatData.push([maxLat - y * dLat, minLng + x * dLng, row[x]])
                        }
                    }
                }
            }
            this.heatLayer.setOptions({ maxZoom:this.map?.getZoom() || 0 })
        }
        this.heatLayer.setLatLngs(heatData)
        // if facet is active, declare it as valid to prevent deadlock (=non-resettable facet) when navigating to a map region not containing bucket values
        this.valid = heatData.length > 0 || this.active
    }

    applyAggregationQuery(facets: Record<string, QueryFacet>) {
        facets[this.indexField] = { field: this.indexField, type: 'heatmap', geom: this.boundsToRange() }
    }

    applyFilterQuery(query: string[]) {
        query.push(`${this.indexField}:${this.boundsToRange()}`)
    }

    reset() {
        this.map.setZoom(0)
        this.mapBounds = worldBounds
        this.active = false
        this.dispatchEvent(new Event('change', { bubbles: true }))
    }

    boundsToRange(): string {
        const west = Math.min(180, Math.max(-180, this.mapBounds.getWest()))
        const east = Math.min(180, Math.max(-180, this.mapBounds.getEast()))
        const south = Math.min(90, Math.max(-90, this.mapBounds.getSouth()))
        const north = Math.min(90, Math.max(-90, this.mapBounds.getNorth()))
        return `[${south},${west} TO ${north},${east}]`
    }

    render() {
        return html`
            <rokit-collapsible>
                <span class="material-icons icon" slot="prefix">location_on</span>
                <div id="label" slot="label">${this.label}</div>
                <div id="map"></div>
                ${!this.active ? nothing : html `
                    <rokit-button class="clear" icon title="Clear" slot="pre-suffix" @click="${(ev: Event) => { ev.stopPropagation(); this.reset() }}"></rokit-button>
                `}
            </rokit-collapsible>
        `
    }
}
