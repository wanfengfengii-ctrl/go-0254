import './style.css';

const app = document.querySelector('#app');
let activeMissionId = null;

const STATES = [
  'pending_lock', 'pending_authorization', 'leases_held', 'route_checking',
  'margin_checking', 'pending_vessel_confirmation', 'launchable', 'launched',
  'engineering_isolation', 'cancelled',
];

function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'text') node.textContent = v;
    else if (k === 'html') node.innerHTML = v;
    else node.setAttribute(k, v);
  }
  for (const c of children) node.append(c);
  return node;
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}

function stateMeter(state) {
  const idx = STATES.indexOf(state);
  const bar = el('div', { class: 'meter' });
  STATES.forEach((s, i) => {
    const step = el('span', {
      class: 'meter-step' + (i <= idx ? ' on' : '') + (s === state ? ' current' : ''),
      text: s,
    });
    bar.append(step);
  });
  return bar;
}

function render(detail) {
  const state = detail?.state;
  const mission = detail || null;

  const health = el('section', {}, el('h2', { text: 'Backend' }));
  const missionSection = el('section', {}, el('h2', { text: 'Active Mission' }));

  if (!mission) {
    missionSection.append(el('p', { class: 'muted', text: 'No mission loaded. Lock a new mission below.' }));
  } else {
    missionSection.append(
      el('dl', {},
        el('dt', { text: 'Mission' }), el('dd', { text: mission.mission_id }),
        el('dt', { text: 'Generation' }), el('dd', { text: mission.generation }),
        el('dt', { text: 'State' }), el('dd', { text: state }),
        el('dt', { text: 'Voyage' }), el('dd', { text: mission.voyage_id }),
        el('dt', { text: 'Hull' }), el('dd', { text: mission.auv_hull_id }),
        el('dt', { text: 'Beacon Slot' }), el('dd', { text: mission.beacon_slot_id }),
        el('dt', { text: 'Route Prefix' }), el('dd', { text: mission.route_prefix }),
        el('dt', { text: 'Vessel Confirmed' }), el('dd', { text: mission.vessel_confirmed }),
        el('dt', { text: 'Review Complete' }), el('dd', { text: mission.review_complete }),
        el('dt', { text: 'Leases' }), el('dd', { text: (mission.leases || []).length }),
        el('dt', { text: 'Segment Evidence' }), el('dd', { text: (mission.segment_evidence || []).length }),
        el('dt', { text: 'Adapter Attempts' }), el('dd', { text: (mission.adapter_attempts || []).length }),
        el('dt', { text: 'Reviews' }), el('dd', { text: (mission.reviews || []).length }),
        el('dt', { text: 'Credential' }), el('dd', { text: mission.final_credential || '—' }),
      ),
      stateMeter(state),
      el('div', { class: 'actions' },
        el('button', { text: 'Authorize', 'data-step': 'authorize' }),
        el('button', { text: 'Acquire Leases', 'data-step': 'leases' }),
        el('button', { text: 'Simulate Route', 'data-step': 'simulate' }),
        el('button', { text: 'Check Margins', 'data-step': 'margins' }),
        el('button', { text: 'Confirm Vessel', 'data-step': 'vessel' }),
        el('button', { text: 'Review', 'data-step': 'review' }),
        el('button', { text: 'Finalize Launch', 'data-step': 'finalize' }),
      ),
    );
  }

  const lockSection = el('section', {},
    el('h2', { text: 'New Mission Lock' }),
    el('form', { id: 'lock-form' },
      el('input', { name: 'voyage_id', value: 'VGR-01', 'aria-label': 'Voyage ID' }),
      el('input', { name: 'auv_hull_id', value: 'HULL-01', 'aria-label': 'Hull ID' }),
      el('input', { name: 'beacon_slot_id', value: 'BEACON-01', 'aria-label': 'Beacon Slot' }),
      el('button', { type: 'submit', text: 'Lock Mission' }),
    ),
    el('pre', { id: 'result', class: 'muted' }),
  );

  app.replaceChildren(
    el('header', {}, el('h1', { text: 'Abyssal AUV Release Gate' }), el('span', { class: 'badge', text: 'live' })),
    el('main', {}, health, missionSection, lockSection),
  );

  document.querySelector('#lock-form')?.addEventListener('submit', onLock);
  document.querySelectorAll('.actions button').forEach((b) =>
    b.addEventListener('click', () => step(b.dataset.step)),
  );
}

const WAYPOINTS = [
  { seq: 1, latitude_microdeg: -12300000, longitude_microdeg: 4500000, target_depth_m: 3000, max_speed_cm_s: 120, expected_draw_wh: 4000, dwell_seconds: 60 },
  { seq: 2, latitude_microdeg: -12350000, longitude_microdeg: 4520000, target_depth_m: 3500, max_speed_cm_s: 120, expected_draw_wh: 5000, dwell_seconds: 60 },
];

async function onLock(e) {
  e.preventDefault();
  const fd = new FormData(e.target);
  const { status, data } = await api('/api/missions', {
    method: 'POST',
    body: {
      voyage_id: fd.get('voyage_id'),
      auv_hull_id: fd.get('auv_hull_id'),
      beacon_slot_id: fd.get('beacon_slot_id'),
      sea_state_revision: 1,
      return_threshold_wh: 10000,
      depth_limit_m: 4000,
      trim_min_g: -400,
      trim_max_g: 400,
      waypoints: WAYPOINTS,
    },
  });
  document.querySelector('#result').textContent = JSON.stringify({ status, data }, null, 2);
  if (status === 201) {
    activeMissionId = data.mission_id;
    await loadMission();
  }
}

async function step(name) {
  if (!activeMissionId) return;
  const base = `/api/missions/${activeMissionId}`;
  let res;
  switch (name) {
    case 'authorize':
      await api(`${base}/authorizations`, { method: 'POST', body: { generation: 1, op_key: 'ui-auth-1', signer_id: 'eng-alice', role: 'technical_authorizer', payload_hash: null } });
      res = await api(`${base}/authorizations`, { method: 'POST', body: { generation: 1, op_key: 'ui-auth-2', signer_id: 'eng-bob', role: 'safety_authorizer', payload_hash: null } });
      break;
    case 'leases':
      res = await api(`${base}/leases/acquire`, { method: 'POST', body: { generation: 1, op_key: 'ui-lease' } });
      break;
    case 'simulate':
      for (let s = 1; s <= 1; s++) {
        res = await api(`${base}/segments/${s}/simulate`, { method: 'POST', body: { generation: 1, op_key: `ui-seg-${s}`, simulator_script_ref: 's', adapter_status: 'success', verdict: 'pass' } });
      }
      break;
    case 'margins':
      res = await api(`${base}/margins/check`, { method: 'POST', body: { generation: 1, op_key: 'ui-margin' } });
      break;
    case 'vessel':
      res = await api(`${base}/vessel-confirmations`, { method: 'POST', body: { generation: 1, op_key: 'ui-vessel', adapter_status: 'success', response_hash: 'reply' } });
      break;
    case 'review':
      for (const [i, r] of [['eng-alice', 'route'], ['eng-alice', 'margins'], ['sea-carol', 'vessel_confirmation'], ['sea-carol', 'sea_state']].entries()) {
        res = await api(`${base}/reviews`, { method: 'POST', body: { generation: 1, op_key: `ui-rev-${i}`, reviewer_id: r[0], review_kind: r[1], decision: 'pass', evidence_hash: 'e' } });
      }
      break;
    case 'finalize':
      res = await api(`${base}/finalize`, { method: 'POST', body: { generation: 1, op_key: 'ui-fin', result: 'launch' } });
      break;
  }
  if (res) document.querySelector('#result').textContent = JSON.stringify(res, null, 2);
  await loadMission();
}

async function loadMission() {
  if (!activeMissionId) return render(null);
  const { status, data } = await api(`/api/missions/${activeMissionId}`);
  if (status === 200) render(data);
  else render(null);
}

loadMission();
