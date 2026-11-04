// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';
import starlightLlmsTxt from 'starlight-llms-txt';

// https://astro.build/config
export default defineConfig({
	site: 'https://sergiu.dev',
	base: '/auth0-mock',
	integrations: [
		starlight({
			title: 'auth0-mock',
			description: 'Drop-in mock of the Auth0 HTTP API for tests and local development.',
			social: [
				{ icon: 'github', label: 'GitHub', href: 'https://github.com/sergiught/auth0-mock' },
			],
			customCss: ['./src/styles/custom.css'],
			editLink: {
				baseUrl: 'https://github.com/sergiught/auth0-mock/edit/main/docs/site/',
			},
			sidebar: [
				{ label: 'Getting started', items: [{ autogenerate: { directory: 'getting-started' } }] },
				{ label: 'Quickstarts', items: [{ autogenerate: { directory: 'quickstarts' } }] },
				{ label: 'Guides', items: [{ autogenerate: { directory: 'guides' } }] },
				{ label: 'Reference', items: [{ autogenerate: { directory: 'reference' } }] },
			],
			plugins: [starlightLinksValidator(), starlightLlmsTxt()],
			head: [
				{
					tag: 'meta',
					attrs: { property: 'og:image', content: 'https://sergiu.dev/auth0-mock/og.png' },
				},
				{
					tag: 'meta',
					attrs: { name: 'twitter:card', content: 'summary_large_image' },
				},
			],
		}),
	],
});
