import http from 'http'
import { parse } from 'querystring'
import { validate } from './validator.ts'

const port = 8000

function checkSingleFormParamExists(form: Record<string, string | string[] | undefined>, param: string): boolean {
    return typeof form[param] === 'string' && form[param].length > 0
}

const server = http.createServer()
server.on('request', (req, res) => {
    if (req.method === 'GET' && req.url === '/healthz') {
        res.writeHead(200)
        res.end('ok')
        return
    }
    if (req.method === 'POST') {
        let body = ''
        req.on('data', chunk => {
            body += chunk.toString()
        })
        req.on('end', async () => {
            const form = parse(body)
            res.setHeader("Content-Type", "application/json")

            for (const param of ['shapesGraph', 'shapeID', 'dataGraph', 'dataID']) {
                if (!checkSingleFormParamExists(form, param)) {
                    res.writeHead(400)
                    res.end(`{ "error": "Missing required unique paramater '${param}'" }`)
                    return
                }
            }

            try {
                const conforms = await validate(form.shapesGraph as string, form.shapeID as string, form.dataGraph as string, form.dataID as string, form.clearCache as string)
                const response = JSON.stringify(conforms)
                res.writeHead(200)
                res.end(response)
                console.log('validation results against', form.shapeID, ':', conforms)
            } catch(e) {
                console.error('error validating', form)
                res.writeHead(500)
                res.end(`{ "error": ${JSON.stringify(e)} }`)
            }
        })
    } else {
        // method not allowed
        res.writeHead(405)
        res.end()
    }
})

server.listen(port, () => {
    console.log(`Server is running on port ${port}`)
})
