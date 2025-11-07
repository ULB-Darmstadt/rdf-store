import { css, html, LitElement } from "lit"
import { customElement } from "lit/decorators.js"

@customElement('layout-header')
export class Header extends LitElement {
    static styles = css`
    :host { display: flex; padding: 10px; color: #333; background-color: #F5F5F5; }
    #title { flex-grow: 1; }
    #title h1 { font-size: 30px; margin: 0; display: flex; align-items: center; }
    #title h2 { font-size: 1rem; font-weight: normal; opacity: 0.7; margin: 0; }
    `

    render() {
        return html`
            <div id="title">
                <h1>RDF store</h1>
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
        justify-content: flex-end;
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
        `
    }
}