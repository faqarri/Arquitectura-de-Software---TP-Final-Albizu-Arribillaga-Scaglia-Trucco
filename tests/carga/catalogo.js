// Pruebas de lectura de Catálogo: detalle con y sin caché (ADR-007), búsqueda
// y retraso del índice (ADR-006). Escenario B de ADR-013.
//
// Variables:
//   BASE_URL   Catálogo (por defecto http://catalogo:8080: el alias de red de las
//              dos instancias, dentro de la red de compose). Sin gateway todavía,
//              las rutas de admin se llaman con X-Usuario-Rol: admin.
//   ESCENARIO  humo | detalle | busqueda | retraso
//   CANTIDAD   ofertas de prueba (por defecto 1000, el máximo que se puede recorrer en la búsqueda)
//
// Las ofertas de prueba llevan "k6carga" en el título; el setup crea las que falten.
//
// Ejemplo (desde la raíz del repo, con el entorno levantado):
//   docker run --rm -i --network <proyecto>_default -e ESCENARIO=detalle \
//     -v "$PWD/tests/carga:/scripts" grafana/k6:0.54.0 run /scripts/catalogo.js
//
// Caché antes y después (ADR-007): correr ESCENARIO=detalle con CACHE_HABILITADA=false
// y con CACHE_HABILITADA=true (docker compose up -d catalogo-1 catalogo-2 entre medio).
import http from 'k6/http';
import { check, sleep, fail } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import exec from 'k6/execution';

const BASE_URL = __ENV.BASE_URL || 'http://catalogo:8080';
const ESCENARIO = __ENV.ESCENARIO || 'humo';
const CANTIDAD = Math.min(parseInt(__ENV.CANTIDAD || '1000', 10), 1000);
const ADMIN = { headers: { 'Content-Type': 'application/json', 'X-Usuario-Rol': 'admin' } };
const DESTINOS = ['Bariloche', 'Córdoba', 'Mendoza', 'Salta', 'Ushuaia', 'Mar del Plata', 'Iguazú', 'El Calafate', 'Jujuy', 'San Martín de los Andes'];
const TIPOS = ['TRANSPORTE', 'ESTADIA', 'EXCURSION'];

const aciertosCache = new Rate('cache_aciertos');
const retrasoBusqueda = new Trend('busqueda_retraso_ms', true);
const sinAparecer = new Counter('busqueda_cambios_sin_aparecer');

const ESCENARIOS = {
  humo: {
    humo: { executor: 'shared-iterations', vus: 1, iterations: 5, exec: 'humo' },
  },
  // ADR-007: 50 usuarios virtuales, 30 s de precalentamiento y 3 min de medición.
  detalle: {
    precalentamiento: { executor: 'constant-vus', vus: 50, duration: '30s', exec: 'detalle', tags: { fase: 'precalentamiento' } },
    medicion: { executor: 'constant-vus', vus: 50, duration: __ENV.DURACION || '3m', startTime: '30s', exec: 'detalle', tags: { fase: 'medicion' } },
  },
  busqueda: {
    busqueda: { executor: 'constant-vus', vus: 20, duration: __ENV.DURACION || '2m', exec: 'busqueda' },
  },
  // ADR-006: 100 cambios, uno por vez, midiendo cuándo aparece cada uno en la búsqueda.
  retraso: {
    retraso: { executor: 'shared-iterations', vus: 1, iterations: 100, maxDuration: '20m', exec: 'retraso' },
  },
};

export const options = {
  setupTimeout: '10m',
  scenarios: ESCENARIOS[ESCENARIO] || fail(`ESCENARIO desconocido: ${ESCENARIO}`),
  summaryTrendStats: ['avg', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  thresholds: {
    // Solo la fase de medición cuenta para los objetivos.
    'http_req_duration{fase:medicion}': ['p(95)<200'],
    'http_req_failed{fase:medicion}': ['rate<0.01'],
    'http_req_duration{scenario:busqueda}': ['p(95)<300'],
    // ADR-006: p95 ≤ 5 s y ningún cambio sin aparecer en 30 s.
    busqueda_retraso_ms: ['p(95)<5000'],
    busqueda_cambios_sin_aparecer: ['count==0'],
  },
};

function cuerpoOferta(i, precio) {
  const tipo = TIPOS[i % TIPOS.length];
  const dia = 1 + (i % 28);
  const mes = String(1 + (i % 12)).padStart(2, '0');
  const o = {
    tipo,
    titulo: `k6carga ${tipo.toLowerCase()} ${i}`,
    descripcion: 'Oferta generada por la prueba de carga k6carga',
    destino: DESTINOS[i % DESTINOS.length],
    desde: `2027-${mes}-${String(dia).padStart(2, '0')}T10:00:00Z`,
    hasta: `2027-${mes}-${String(dia).padStart(2, '0')}T20:00:00Z`,
    precio: precio || 10000 + (i % 500) * 100,
    cupoTotal: 100,
  };
  if (tipo === 'TRANSPORTE') {
    o.subtipoTransporte = ['AVION', 'TREN', 'COLECTIVO'][i % 3];
    o.origen = 'Buenos Aires';
  }
  return o;
}

function idsIndexados() {
  const ids = [];
  for (let pagina = 1; pagina <= 20; pagina++) {
    const r = http.get(`${BASE_URL}/api/busqueda?q=k6carga&tamanio=50&pagina=${pagina}&orden=fecha`);
    if (r.status !== 200) fail(`búsqueda ${r.status}: ${r.body}`);
    const p = r.json();
    p.items.forEach((o) => ids.push(o.id));
    if (ids.length >= p.total || p.items.length === 0) break;
  }
  return ids;
}

export function setup() {
  let ids = idsIndexados();
  const faltan = CANTIDAD - ids.length;
  if (faltan > 0) {
    console.log(`creando ${faltan} ofertas de prueba`);
    for (let desde = 0; desde < faltan; desde += 50) {
      const lote = [];
      for (let i = desde; i < Math.min(desde + 50, faltan); i++) {
        lote.push(['POST', `${BASE_URL}/api/ofertas`, JSON.stringify(cuerpoOferta(ids.length + i)), ADMIN]);
      }
      http.batch(lote).forEach((r) => { if (r.status !== 201) fail(`crear oferta ${r.status}: ${r.body}`); });
    }
    // Esperar a que el indexador las lleve a la búsqueda.
    for (let i = 0; i < 60 && ids.length < CANTIDAD; i++) {
      sleep(1);
      ids = idsIndexados();
    }
  }
  if (ids.length < CANTIDAD) fail(`solo ${ids.length} de ${CANTIDAD} ofertas en la búsqueda`);
  console.log(`${ids.length} ofertas de prueba listas`);
  return { ids: ids.slice(0, CANTIDAD) };
}

// El 20 % de las ofertas recibe el 80 % de las lecturas (ADR-007).
function idSesgado(ids) {
  const calientes = Math.max(1, Math.floor(ids.length * 0.2));
  if (Math.random() < 0.8) return ids[Math.floor(Math.random() * calientes)];
  return ids[calientes + Math.floor(Math.random() * (ids.length - calientes))];
}

export function detalle(datos) {
  const r = http.get(`${BASE_URL}/api/ofertas/${idSesgado(datos.ids)}`, { tags: { name: 'GET /api/ofertas/{id}' } });
  check(r, { 'detalle 200': (x) => x.status === 200 });
  aciertosCache.add(r.headers['X-Cache'] === 'HIT');
}

function al(lista) { return lista[Math.floor(Math.random() * lista.length)]; }

export function busqueda() {
  const p = [`pagina=${1 + Math.floor(Math.random() * 3)}`];
  if (Math.random() < 0.7) p.push(`destino=${encodeURIComponent(al(DESTINOS))}`);
  if (Math.random() < 0.5) p.push(`tipo=${al(TIPOS)}`);
  if (Math.random() < 0.3) p.push('precioMin=20000&precioMax=40000');
  if (Math.random() < 0.3) p.push('desde=2027-03-01&hasta=2027-08-31');
  if (Math.random() < 0.2) p.push('q=k6carga');
  p.push(`orden=${al(['fecha', 'precio', '-precio'])}`);
  const r = http.get(`${BASE_URL}/api/busqueda?${p.join('&')}`, { tags: { name: 'GET /api/busqueda' } });
  check(r, { 'búsqueda 200': (x) => x.status === 200 });
}

// Cambia el precio de una oferta a un valor único y consulta la búsqueda cada
// 100 ms hasta que aparece (ADR-006, "Prueba de punta a punta").
export function retraso(datos) {
  const n = exec.scenario.iterationInTest;
  const id = datos.ids[n % datos.ids.length];
  const precio = 900000 + Date.now() % 100000 + n / 100; // único por iteración
  const inicio = Date.now();
  const r = http.put(`${BASE_URL}/api/ofertas/${id}`, JSON.stringify(cuerpoOferta(n, precio)), ADMIN);
  if (!check(r, { 'edición 200': (x) => x.status === 200 })) return;
  while (Date.now() - inicio < 30000) {
    const b = http.get(`${BASE_URL}/api/busqueda?q=k6carga&precioMin=${precio}&precioMax=${precio}`, { tags: { name: 'sondeo' } });
    if (b.status === 200 && b.json().items.some((o) => o.id === id)) {
      retrasoBusqueda.add(Date.now() - inicio);
      return;
    }
    sleep(0.1);
  }
  sinAparecer.add(1);
}

export function humo(datos) {
  detalle(datos);
  busqueda();
}
