import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import { createApi } from './lib/api';
import { browserHistory } from './lib/history';
import { Router } from './lib/router.svelte';

const target = document.getElementById('app');
if (!target) {
	throw new Error('index.html has no #app element');
}

export default mount(App, {
	target,
	props: { api: createApi(), router: new Router(browserHistory()) }
});
