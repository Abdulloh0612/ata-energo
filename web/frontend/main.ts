import { initialize } from '../static/js/app.js';
import { renderOverview } from './overview';
import { renderWork } from './work';
import { initializeSearch } from './search';
import './design.css';

initialize(renderOverview, renderWork);
initializeSearch();
