import { defineConfig, loadEnv } from 'vite'

export default defineConfig(({ mode}) => {
        // Load env file based on `mode` in the current working directory.
        // Set the third parameter to '' to load all env regardless of the
        // `VITE_` prefix.
        const env = loadEnv(mode, process.cwd(), '')
        return {
            define: {
                // Provide an explicit app-level constant derived from an env var.
                __APP_ENV__: JSON.stringify(env.APP_ENV),
            },
            base: env.BASE_URL ?? '/'
        }
    })