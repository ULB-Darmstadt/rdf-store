import { css, html, LitElement } from "lit"
import { customElement } from "lit/decorators.js"
import { BACKEND_URL } from "../../constants"
import { i18n, registerLabel } from "../../i18n"
import { DataFactory } from "n3"

document.title = 'Knowledge Graph Explorer'
const style = document.createElement('style')
style.innerText = `:root { --rokit-primary-color: #003273; }`
document.head.appendChild(style)

registerLabel('sub_heading', [ DataFactory.literal('Your portal to the NFDI4ING Knowledge Graph', 'en'), DataFactory.literal('Ihr Portal zum NFDI4ING Wissensgraph', 'de') ])
registerLabel('service_provided_by', [ DataFactory.literal('This service is provided by University and State Library Darmstadt', 'en'), DataFactory.literal('Dieser Dienst wird von der Universitäts- und Landesbibliothek Darmstadt bereitgestellt', 'de') ])
registerLabel('dfg_hint', [ DataFactory.literal('NFDI4ING is supported by DFG under project number', 'en'), DataFactory.literal('NFDI4ING wird gefördert durch die Deutsche Forschungsgemeinschaft (DFG) - Projektnummer', 'de') ])

@customElement('layout-header')
export class Header extends LitElement {
    static styles = css`
    :host { color: #FFF; background-color: #00689d; display: flex; padding: 10px; }
    #title { flex-grow: 1; display: flex; }
    #title img { height: 30px; margin-right: 12px; margin-top: 6px; }
    #title h1 { font-size: 20px; margin: 0; display: flex; align-items: center; }
    #title h2 { font-size: 0.8rem; font-weight: normal; opacity: 0.7; margin: 0; }
    `

    render() {
        return html`
            <div id="title">
                <img src="${new URL('../_shared/NFDI4ING_Wort-Bildmarke_NEG_RGB-DEh5SvlN.png', import.meta.url).href}">
                <div>
                    <h1>Knowledge Graph Explorer</h1>
                    <h2>${i18n['sub_heading']}</h2>
                </div>
            </div>
            <slot></slot>
        `
    }
}

@customElement('layout-footer')
export class Footer extends LitElement {
    static styles = css`
    .logos {
        display: flex;
        justify-content: space-between;
        padding: 10px;
        gap: 10px;

        img { height: 60px; }
        span, a { font-size: 12px; color: #0009; }
    }
    a { color: inherit; }
    .d-flex { display: flex; }
    .legal-info { display: flex; background-color: #F5F5F5; padding: 3px 5px; font-size: 0.8em; }
    .spacer { flex-grow: 1; }
    @media (max-width: 767px) {
        .logos { flex-direction: column; }
    }
    @media (min-width: 768px) {
        .logos { align-items: flex-end; }
        .logos > :last-child { display: flex; flex-direction: column; justify-content: flex-end; }
    }
    `

    render() {
        return html`
        <div class="logos">
            <div>
                <div class="d-flex">
                    <a href="https://www.ulb.tu-darmstadt.de"><img src="${new URL('../_shared/ULB_RGB-B9igTOm_.jpg', import.meta.url).href}" alt="ULB logo"></a>
                    <a href="https://www.tu-darmstadt.de"><img src="${new URL('../_shared/tud_logo-DY5K59Zv.png', import.meta.url).href}" alt="TUD logo"></a>
                </div>
                <span>${i18n['service_provided_by']}</span>
            </div>
            <div><a href="https://www.ulb.tu-darmstadt.de/impressum.de.jsp">${i18n['imprint']}</a></div>
            <div><a href="https://www.tu-darmstadt.de/datenschutzerklaerung.de.jsp">${i18n['privacy']}</a></div>
            <div>
                <div class="d-flex">
                    <a href="https://www.nfdi4ing.de"><img class="mt-1" src="${new URL('../_shared/NFDI4ING_Wort-Bildmarke_POS_RGB-BflV2Uxx.svg', import.meta.url).href}" alt="NFDI4ING logo"></a>
                    <a href="https://www.dfg.de"><img src="${new URL('../_shared/dfg_logo_schriftzug_blau_foerderung_4c-DXLBvhFM.jpg', import.meta.url).href}" alt="DFG logo"></a>
                </div>
                <span>${i18n['dfg_hint']} <a href="https://gepris.dfg.de/gepris/projekt/442146713?context=projekt&task=showDetail&id=442146713&">442146713</a></span>
            </div>
        </div>
        <div class="legal-info">
            <span class="spacer"></span>
            <rokit-button text dense href="${BACKEND_URL}/">${i18n['api_documentation']}</rokit-button>
        </div>
        `
    }
}