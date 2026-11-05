import { mount } from 'svelte';
import './styles/tokens.css';
import './styles/base.css';
import './styles/forms.css';
import App from './App.svelte';

mount(App, { target: document.getElementById('app')! });
