import { customElement } from 'lit/decorators.js'
import { NumberRangeFacet } from './number-range'
import { epochToDate } from '@ro-kit/ui-widgets'

@customElement('date-range-facet')
export class DateRangeFacet extends NumberRangeFacet {

    constructor(indexField: string) {
        super(indexField)
        this.icon = 'date_range'
    }

    firstUpdated() {
        super.firstUpdated()
        this.slider!.labelFormatter = (value) => { return epochToDate(value, true) }
    }

    updateValues(aggs: Record<string, string>) {
        // convert dates to epoch milliseconds so that parent class can handle all the facet logic
        const numbered: Record<string, number> =  {}
        Object.entries(aggs).forEach((value) => {
            numbered[value[0]] = new Date(value[1]).getTime() / 1000
        })
        super.updateValues(numbered)
    }

    applyFilterQuery(query: string[]) {
        if (this.value?.length === 2) {
            query.push(`+${this.indexField}:[${epochToDate(this.value[0])} TO ${epochToDate(this.value[1])}]`)
        }
    }
}
