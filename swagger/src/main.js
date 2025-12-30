import swagger from 'swagger-ui-dist'
import 'swagger-ui-dist/swagger-ui.css'

const APP_PATH = import.meta.env.BASE_URL ?? '/'
const BACKEND_URL = `${process.env.NODE_ENV === 'production' ? APP_PATH : 'http://localhost:3000/'}`

swagger.SwaggerUIBundle({
  dom_id: '#app',
  url: `${BACKEND_URL}/openapi.json`
})