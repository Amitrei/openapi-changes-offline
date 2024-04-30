
import React from 'react';
import ReactDOM from 'react-dom/client';
import { OpenAPIChanges } from "@/OpenAPIChanges";
import './index.css';
import * as monaco from 'monaco-editor/esm/vs/editor/editor.api.js';
import { loader } from '@monaco-editor/react';

loader.config({ monaco });

const root = ReactDOM.createRoot(
  document.getElementById('root') as HTMLElement
);

//import data from '../data.json'
let data: any
root.render(
  <React.StrictMode>
    <OpenAPIChanges report={data} />
  </React.StrictMode>
);
