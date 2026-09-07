import { createRoot } from 'react-dom/client';
import { App } from './App';
import { api } from './bridge';
import { Controller } from './controller';
import './style.css';

const controller = new Controller(api);
createRoot(document.getElementById('root')!).render(<App controller={controller} />);
