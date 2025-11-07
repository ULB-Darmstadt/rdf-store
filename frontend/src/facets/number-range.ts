import { html } from 'lit'
import { customElement, property, query } from 'lit/decorators.js'

import { RokitSlider } from '@ro-kit/ui-widgets'
import { Facet } from './base'

@customElement('number-range-facet')
export class NumberRangeFacet extends Facet {
    @query('#slider')
    slider!: RokitSlider
    @property()
    min?: number
    @property()
    max?: number
    @property()
    value?: number[]
    lastSelectedValue?: number[]
    icon = 'pin'
    step?: string

    constructor(indexField: string, step?: string) {
        super(indexField)
        this.step = step
    }

    firstUpdated() {
        this.slider.value = this.value ? JSON.stringify(this.value) : ''
        this.slider.step = this.step
    }

    updateValues(aggs: Record<string, number | string>) {
        if (!this.valid || !this.active) {
            this.min = aggs[`${this.indexField}_min`] as number
            this.max = aggs[`${this.indexField}_max`] as number
        }
        this.valid = this.min !== undefined && isFinite(this.min) && this.max !== undefined && isFinite(this.max) && this.min < this.max
        if (this.valid) {
            if (this.lastSelectedValue !== undefined) {
                this.value = [
                    (this.lastSelectedValue[0] < this.min!) ? this.min! : this.lastSelectedValue[0],
                    (this.lastSelectedValue[1] > this.max!) ? this.max! : this.lastSelectedValue[1]
                ]
            } else {
                this.value = undefined
            }
        }
        // first let the new value update pass into the DOM and then update slider value to prevent glitches
        setTimeout(() => {
            if (this.slider) {
                this.slider.value = this.value ? JSON.stringify(this.value) : ''
            }
        })
        this.updateActive()
    }

    onChange() {
        this.value = this.slider.value ? JSON.parse(this.slider.value) : undefined
        this.lastSelectedValue = this.value ? [...this.value] : undefined
        this.updateActive()
        this.dispatchEvent(new Event('change', { bubbles: true }))
    }

    applyAggregationQuery(facets: Record<string, string>) {
        facets[`${this.indexField}_min`] = `min(${this.indexField})`
        facets[`${this.indexField}_max`] = `max(${this.indexField})`
    }

    applyFilterQuery(query: string[]) {
        if (this.value) {
            query.push(`${this.indexField}:[${this.value[0]} TO ${this.value[1]}]`)
        }
    }

    updateActive() {
        this.active = this.valid && (this.value !== undefined && (this.value[0] > this.min! || this.value[1] < this.max!))
    }

    render() {
        return  html`
            <rokit-slider id="slider" title="${this.title}" label="${this.label}" class="w-100" min="${this.min}" max="${this.max}" @change="${() => this.onChange()}" range clearable>
                <span class="material-icons icon" slot="prefix">${this.icon}</span>
            </rokit-slider>
        `
    }
}
