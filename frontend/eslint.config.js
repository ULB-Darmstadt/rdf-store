import js from '@eslint/js'
import tsPlugin from '@typescript-eslint/eslint-plugin'
import tsParser from '@typescript-eslint/parser'
import globals from 'globals'
import ts from 'typescript'

const [, tsEslintRecommended, tsRecommended] = tsPlugin.configs['flat/recommended']
const decoratorAlignment = {
    meta: {
        type: 'layout',
        fixable: 'whitespace',
        schema: [],
        messages: {
            misaligned: 'Decorated class members must be aligned with their decorators.'
        }
    },
    create(context) {
        const sourceCode = context.sourceCode

        return {
            'PropertyDefinition[decorators.length>0], MethodDefinition[decorators.length>0]'(node) {
                const decorators = node.decorators
                const memberToken = sourceCode.getTokenAfter(decorators.at(-1))

                if (!memberToken || memberToken.loc.start.line === decorators.at(-1).loc.end.line) {
                    return
                }

                const expectedColumn = decorators[0].loc.start.column
                if (memberToken.loc.start.column === expectedColumn) {
                    return
                }

                context.report({
                    loc: memberToken.loc,
                    messageId: 'misaligned',
                    fix(fixer) {
                        const lineStart = memberToken.range[0] - memberToken.loc.start.column
                        return fixer.replaceTextRange(
                            [lineStart, memberToken.range[0]],
                            ' '.repeat(expectedColumn)
                        )
                    }
                })
            }
        }
    }
}
const litTemplateFiles = new Map()
const litTemplateProcessor = {
    meta: {
        name: 'lit-templates',
        version: '1.0.0'
    },
    supportsAutofix: true,
    preprocess(text, filename) {
        const sourceFile = ts.createSourceFile(
            filename,
            text,
            ts.ScriptTarget.Latest,
            true,
            ts.ScriptKind.TS
        )
        const ranges = []

        const visit = (node) => {
            if (
                ts.isTaggedTemplateExpression(node) &&
                ts.isIdentifier(node.tag) &&
                (node.tag.text === 'html' || node.tag.text === 'css')
            ) {
                ranges.push([
                    node.template.getStart(sourceFile) + 1,
                    node.template.end - 1
                ])
                return
            }

            ts.forEachChild(node, visit)
        }

        visit(sourceFile)
        litTemplateFiles.set(filename, { ranges, sourceFile })
        return [text]
    },
    postprocess(messageLists, filename) {
        const file = litTemplateFiles.get(filename)
        litTemplateFiles.delete(filename)

        if (!file) {
            return messageLists[0]
        }

        return messageLists[0].filter((message) => {
            const start = message.fix?.range[0] ??
                file.sourceFile.getPositionOfLineAndCharacter(
                    message.line - 1,
                    message.column - 1
                )
            const end = message.fix?.range[1] ?? start

            return !file.ranges.some(([rangeStart, rangeEnd]) =>
                start < rangeEnd && end >= rangeStart
            )
        })
    }
}

export default [
    {
        ignores: ['dist/**', 'node_modules/**']
    },
    {
        files: ['src/**/*.{ts,tsx}'],
        languageOptions: {
            parser: tsParser,
            parserOptions: {
                ecmaVersion: 'latest',
                sourceType: 'module',
                ecmaFeatures: {
                    jsx: true
                }
            },
            globals: {
                ...globals.browser,
                ...globals.es2020,
                ...globals.node
            }
        },
        plugins: {
            '@typescript-eslint': tsPlugin,
            local: {
                rules: {
                    'decorator-alignment': decoratorAlignment
                },
                processors: {
                    'lit-templates': litTemplateProcessor
                }
            }
        },
        processor: 'local/lit-templates',
        rules: {
            ...js.configs.recommended.rules,
            ...tsEslintRecommended.rules,
            ...tsRecommended.rules,
            indent: ['error', 4, {
                SwitchCase: 1,
                ignoredNodes: [
                    'PropertyDefinition[decorators.length > 0]',
                    'MethodDefinition[decorators.length > 0]'
                ]
            }],
            quotes: ['error', 'single', { avoidEscape: true }],
            semi: ['error', 'never'],
            'comma-dangle': ['error', 'never'],
            'comma-spacing': ['error', { before: false, after: true }],
            curly: ['error', 'all'],
            'brace-style': ['error', '1tbs', { allowSingleLine: false }],
            'key-spacing': ['error', { beforeColon: false, afterColon: true }],
            'keyword-spacing': ['error', { before: true, after: true }],
            'space-before-blocks': 'error',
            'space-infix-ops': 'error',
            'object-curly-spacing': ['error', 'always'],
            'array-bracket-spacing': ['error', 'never'],
            'eol-last': ['error', 'always'],
            'no-multi-spaces': 'error',
            'no-trailing-spaces': 'error',
            'local/decorator-alignment': 'error',
            '@typescript-eslint/no-unused-vars': [
                'error',
                {
                    argsIgnorePattern: '^_',
                    varsIgnorePattern: '^_',
                    caughtErrorsIgnorePattern: '^_'
                }
            ]
        }
    }
]
