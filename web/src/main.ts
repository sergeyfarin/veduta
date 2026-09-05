import { mount } from 'svelte';
import App from './App.svelte';
import './styles/tokens.css';

export default mount(App, { target: document.getElementById('app')! });
