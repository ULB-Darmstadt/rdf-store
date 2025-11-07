import { css, html, LitElement } from "lit"
import { customElement } from "lit/decorators.js"

document.title = 'NFDI4ING semantic knowledge graph'
const style = document.createElement('style')
style.innerText = `:root { --rokit-primary-color: #003273; }`
document.head.appendChild(style)

@customElement('layout-header')
export class Header extends LitElement {
    static styles = css`
    :host { color: #FFF; background-color: #212121; display: flex; padding: 10px; }
    #title { flex-grow: 1; }
    #title img { height: 30px; margin-right: 12px; }
    #title h1 { font-size: 30px; margin: 0; display: flex; align-items: center; }
    #title h2 { font-size: 1rem; font-weight: normal; opacity: 0.7; margin: 0; }
    `

    render() {
        return html`
            <div id="title">
                <h1><img src="${new URL('../_shared/nfdi4ing-logo.png', import.meta.url).href}">Semantic knowledge graph</h1>
                <h2>Faceted search on RDF data that conform to SHACL shapes</h2>
            </div>
            <slot></slot>
        `
    }
}

@customElement('layout-footer')
export class Footer extends LitElement {
    static styles = css`
    :host {
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 10px;
        gap: 10px;
    }
    img { height: 60px; }
    span { font-size: 12px; color: #0009; }
    a { color: inherit; }
    `

    render() {
        return html`
        <div>
            <div>
                <a href="https://www.ulb.tu-darmstadt.de"><img src="${new URL('../_shared/ULB_RGB-B9igTOm_.jpg', import.meta.url).href}" alt="ULB logo"></a>
                <a href="https://www.tu-darmstadt.de"><img src="${new URL('../_shared/tud_logo-DY5K59Zv.png', import.meta.url).href}" alt="TUD logo"></a>
            </div>
            <span>This service is provided by University and State Library Darmstadt</span>
        </div>
        <a href="https://www.nfdi4ing.de"><img src="${new URL('../_shared/NFDI4ING_Wort-Bildmarke_POS_RGB-BflV2Uxx.svg', import.meta.url).href}" alt="NFDI4ING logo"></a>
        <div>
            <div><a href="https://www.tu-darmstadt.de"><img src="${new URL('../_shared/dfg_logo_schriftzug_blau_foerderung_4c-DXLBvhFM.jpg', import.meta.url).href}" alt="DFG logo"></a></div>
            <span>NFDI4ING is supported by DFG under project number <a href="https://gepris.dfg.de/gepris/projekt/442146713?context=projekt&task=showDetail&id=442146713&">442146713</a></span>
        </div>
        `
    }
}