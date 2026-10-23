// Prueba de carga de POST /v1/comprobantes (escenario A de ADR-013).
//
// Variables:
//   BASE_URL   URL del servicio (por defecto http://comprobantes:8080, dentro de la red de compose)
//   API_KEYS   claves separadas por coma; se reparten por iteración, para no medir el límite
//              de 60 solicitudes por minuto por API Key (hacen falta tasa / 1 por segundo claves)
//   ESCENARIO  humo | nominal | creciente | idempotencia | limite
//
// Ejemplo (desde la raíz del repo, con el entorno levantado):
//   docker run --rm -i --network <proyecto>_default -e API_KEYS=... -e ESCENARIO=nominal \
//     -v "$PWD/tests/carga:/scripts" grafana/k6:0.54.0 run /scripts/comprobantes.js
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import exec from 'k6/execution';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const BASE_URL = __ENV.BASE_URL || 'http://comprobantes:8080';
const CLAVES = (__ENV.API_KEYS || '').split(',').filter(Boolean);
const ESCENARIO = __ENV.ESCENARIO || 'humo';

const respuestas = new Counter('respuestas_por_codigo');

const ESCENARIOS = {
  // A1: verificar el script y el entorno.
  humo: { executor: 'constant-arrival-rate', rate: 1, timeUnit: '1s', duration: '1m', preAllocatedVUs: 5 },
  // A2: carga nominal, comparada contra los objetivos.
  nominal: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1s', duration: '5m', preAllocatedVUs: 50 },
  // A3: carga creciente hasta encontrar el límite.
  creciente: {
    executor: 'ramping-arrival-rate', startRate: 10, timeUnit: '1s', preAllocatedVUs: 200, maxVUs: 1000,
    stages: [{ target: 300, duration: __ENV.DURACION_RAMPA || '10m' }],
  },
  // A4: 10 % de las solicitudes repiten una Idempotency-Key ya usada.
  idempotencia: { executor: 'constant-arrival-rate', rate: 20, timeUnit: '1s', duration: '2m', preAllocatedVUs: 50 },
  // A5: una sola API Key, por encima del límite.
  limite: { executor: 'constant-arrival-rate', rate: 2, timeUnit: '1s', duration: '1m', preAllocatedVUs: 5 },
};

const UMBRALES = {
  nominal: { http_req_failed: ['rate<0.001'], http_req_duration: ['p(95)<300', 'p(99)<500'] },
  // En la rampa se corta cuando hay más de 1 % de errores o el p95 supera 1 s.
  creciente: {
    http_req_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '30s' }],
    http_req_duration: [{ threshold: 'p(95)<1000', abortOnFail: true, delayAbortEval: '30s' }],
  },
};

export const options = {
  scenarios: { [ESCENARIO]: ESCENARIOS[ESCENARIO] },
  thresholds: UMBRALES[ESCENARIO] || {},
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// Pares (API Key, Idempotency-Key) ya usados por este usuario virtual, para el escenario de
// idempotencia. La idempotencia es por consumidor: el reintento debe ir con la misma API Key.
const usadas = [];

export default function () {
  let clave = ESCENARIO === 'limite' ? CLAVES[0] : CLAVES[exec.scenario.iterationInTest % CLAVES.length];
  const cuerpo = JSON.stringify({
    email: `cliente${__VU}@example.com`,
    items: [
      { descripcion: 'Excursion x2', precio: 30000 },
      { descripcion: 'Cargo por servicio', precio: 1500 },
    ],
  });

  let idem = uuidv4();
  const repetir = ESCENARIO === 'idempotencia' && usadas.length > 0 && Math.random() < 0.1;
  if (repetir) ({ clave, idem } = usadas[Math.floor(Math.random() * usadas.length)]);

  const r = http.post(`${BASE_URL}/v1/comprobantes`, cuerpo, {
    headers: { 'Content-Type': 'application/json', 'X-API-Key': clave, 'Idempotency-Key': idem },
    tags: { nombre: 'emitir' },
    // En A5 se esperan 429: no cuentan como fallo de la prueba.
    responseCallback: http.expectedStatuses(200, 201, ...(ESCENARIO === 'limite' ? [429] : [])),
  });
  respuestas.add(1, { codigo: String(r.status) });

  if (!repetir) usadas.push({ clave, idem });
  if (ESCENARIO === 'limite') {
    check(r, { 'emitido o limitado (201/429)': (res) => res.status === 201 || res.status === 429 });
  } else if (repetir) {
    check(r, { 'reintento devuelve el mismo comprobante (200)': (res) => res.status === 200 });
  } else {
    check(r, { 'emitido (201)': (res) => res.status === 201 });
  }
}
