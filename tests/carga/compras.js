// Flujo de compra: escenario B de ADR-013 (B1 demanda distribuida y B2 oferta caliente).
//
// Variables:
//   RESERVAS_URL  por defecto http://reservas:8080
//   CATALOGO_URL  por defecto http://catalogo:8080 (alias de las dos instancias)
//   ESCENARIO     humo | distribuida | caliente
//   VUS           usuarios virtuales (por defecto 200, como en ADR-013)
//
// Sin gateway todavía: la identidad va en los headers X-Usuario-Id y
// X-Usuario-Email, como los pondría el gateway. Cada compra confirmada emite un
// comprobante: correr con el correo en Mailpit (SMTP_* vacías), no con Gmail.
//
// Ejemplo:
//   docker run --rm -i --network <proyecto>_default -e ESCENARIO=caliente \
//     -v "$PWD/tests/carga:/scripts" grafana/k6:0.54.0 run /scripts/compras.js
import http from 'k6/http';
import { check, fail } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import exec from 'k6/execution';
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

const RESERVAS = __ENV.RESERVAS_URL || 'http://reservas:8080';
const CATALOGO = __ENV.CATALOGO_URL || 'http://catalogo:8080';
const ESCENARIO = __ENV.ESCENARIO || 'humo';
const VUS = parseInt(__ENV.VUS || '200', 10);
const CUPO_CALIENTE = 50;
const OFERTAS_DISTRIBUIDAS = 500;
const ADMIN = { headers: { 'Content-Type': 'application/json', 'X-Usuario-Rol': 'admin' } };

const retenciones = new Counter('retenciones_por_codigo');
const compras = new Counter('compras_por_codigo');
const retenidasCaliente = new Counter('caliente_retenciones_exitosas');
const confirmadas = new Counter('compras_confirmadas');
const duracionRetener = new Trend('retener_ms', true);
const duracionComprar = new Trend('comprar_ms', true);

const ESCENARIOS = {
  humo: { humo: { executor: 'per-vu-iterations', vus: 2, iterations: 2, exec: 'distribuida' } },
  // B1: muchos usuarios sobre 500 ofertas distintas (poca contención).
  distribuida: { distribuida: { executor: 'constant-vus', vus: VUS, duration: __ENV.DURACION || '2m', exec: 'distribuida' } },
  // B2: todos contra la misma oferta de 50 cupos, a la vez.
  caliente: { caliente: { executor: 'per-vu-iterations', vus: VUS, iterations: 1, maxDuration: '5m', exec: 'caliente' } },
};

export const options = {
  setupTimeout: '5m',
  scenarios: ESCENARIOS[ESCENARIO] || fail(`ESCENARIO desconocido: ${ESCENARIO}`),
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    // RN-01: nunca se retiene ni se vende más que el cupo.
    caliente_retenciones_exitosas: [`count<=${CUPO_CALIENTE}`],
    'checks{verificacion:invariante}': ['rate==1'],
  },
};

function crearOferta(i, cupo) {
  const r = http.post(`${CATALOGO}/api/ofertas`, JSON.stringify({
    tipo: 'EXCURSION', titulo: `k6compra ${i}`, descripcion: 'Oferta de la prueba de compras',
    destino: 'Bariloche', desde: '2027-02-10T10:00:00Z', hasta: '2027-02-10T18:00:00Z',
    precio: 25000, cupoTotal: cupo,
  }), ADMIN);
  if (r.status !== 201) fail(`crear oferta ${r.status}: ${r.body}`);
  return r.json('id');
}

export function setup() {
  if (ESCENARIO === 'caliente') return { caliente: crearOferta('caliente', CUPO_CALIENTE) };
  const n = ESCENARIO === 'humo' ? 4 : OFERTAS_DISTRIBUIDAS;
  const ids = [];
  for (let i = 0; i < n; i++) ids.push(crearOferta(i, 100000));
  return { ids };
}

function usuario() {
  const id = `k6-${exec.vu.idInTest}-${exec.vu.iterationInScenario}-${uuidv4().slice(0, 8)}`;
  return { 'X-Usuario-Id': id, 'X-Usuario-Email': `${id}@carga.example`, 'Content-Type': 'application/json' };
}

function retener(h, ofertaId) {
  const r = http.post(`${RESERVAS}/api/carrito/items`, JSON.stringify({ ofertaId, cantidad: 1 }),
    { headers: { ...h, 'Idempotency-Key': uuidv4() }, tags: { name: 'POST /api/carrito/items' } });
  retenciones.add(1, { codigo: String(r.status) });
  duracionRetener.add(r.timings.duration);
  return r;
}

function comprar(h) {
  const r = http.post(`${RESERVAS}/api/compras`, null,
    { headers: { ...h, 'Idempotency-Key': uuidv4() }, tags: { name: 'POST /api/compras' } });
  compras.add(1, { codigo: String(r.status) });
  duracionComprar.add(r.timings.duration);
  if (r.status === 201) confirmadas.add(1);
  return r;
}

export function distribuida(datos) {
  const h = usuario();
  const ofertaId = datos.ids[Math.floor(Math.random() * datos.ids.length)];
  http.get(`${CATALOGO}/api/ofertas/${ofertaId}/disponibilidad`, { tags: { name: 'GET disponibilidad' } });
  const r = retener(h, ofertaId);
  if (!check(r, { 'retener 201': (x) => x.status === 201 })) return;
  const c = comprar(h);
  check(c, { 'compra 201 o 202': (x) => x.status === 201 || x.status === 202 });
}

export function caliente(datos) {
  const h = usuario();
  const r = retener(h, datos.caliente);
  check(r, { 'retener 201 o 409 sin cupo': (x) => x.status === 201 || x.status === 409 });
  if (r.status !== 201) return;
  retenidasCaliente.add(1);
  comprar(h);
}

// Verifica RN-01 contra la fuente de verdad: vendidos ≤ cupo.
export function teardown(datos) {
  if (!datos.caliente) return;
  const o = http.get(`${CATALOGO}/api/ofertas/${datos.caliente}`, ADMIN).json();
  const d = http.get(`${CATALOGO}/api/ofertas/${datos.caliente}/disponibilidad`).json();
  console.log(`oferta caliente: cupo ${o.cupoTotal}, vendidos ${o.vendidos}, estado ${d.estado}, libres ${d.libres}`);
  check(o, { 'vendidos <= cupo (RN-01)': (x) => x.vendidos <= x.cupoTotal }, { verificacion: 'invariante' });
}
