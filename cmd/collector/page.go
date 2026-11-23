package main

const pageHTML = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Журнал событий</title>
<style>
body {
  margin: 20px;
  color: #222;
  font: 16px Arial, sans-serif;
}

h1 { font-size: 26px; }
h2 { font-size: 19px; }

form {
  display: flex;
  flex-wrap: wrap;
  align-items: end;
  gap: 8px 12px;
  padding: 12px 0;
  border-block: 1px solid #ccc;
}

label {
  display: grid;
  gap: 4px;
  font-size: 14px;
}

input, button { padding: 4px 6px; }
.table { overflow-x: auto; }

table {
  width: 100%;
  border-collapse: collapse;
  text-align: left;
}

th, td {
  border: 1px solid #ccc;
  padding: 6px;
  vertical-align: top;
}

th { background: #eee; }
td:first-child { white-space: nowrap; }
td:last-child { overflow-wrap: anywhere; min-width: 12rem; }
</style>
</head>
<body>
<h1>Журнал событий</h1>
<p>Записано за последнюю секунду: <strong id="write-rate">{{.Rate}}</strong></p>
<p>Записано с запуска коллектора: <strong id="saved-total">{{.Saved}}</strong></p>
<p id="metrics-status" role="status" hidden>Показатели временно недоступны.</p>
<form method="get">
<label>Агент<input name="agent" value="{{.Filters.Get "agent"}}" placeholder="agent-01"></label>
<label>Тип лога<input name="source" value="{{.Filters.Get "source"}}" placeholder="syslog, nginx, json"></label>
<label>Уровень<input name="level" value="{{.Filters.Get "level"}}" placeholder="error"></label>
<label>С (UTC)<input type="datetime-local" name="from" value="{{.Filters.Get "from"}}"></label>
<label>До (UTC)<input type="datetime-local" name="to" value="{{.Filters.Get "to"}}"></label>
<label>Текст<input name="text" value="{{.Filters.Get "text"}}"></label>
<button>Найти</button><a href="/">Сбросить</a>
</form>
<section>
<h2>Записей по агентам</h2>
<ul>{{range $agent,$count := .Counts}}<li>{{$agent}} — {{$count}}</li>{{else}}<li>Пока нет записей</li>{{end}}</ul>
</section>
<section class="table">
<h2>Последние 100 записей</h2>
<table><thead><tr><th>Время (UTC)</th><th>Агент</th><th>Тип</th><th>Уровень</th><th>IP</th><th>Код</th><th>Сообщение</th></tr></thead>
<tbody>{{range .Rows}}<tr><td>{{date .Time}}</td><td>{{.Agent}}</td><td>{{.Source}}</td><td>{{.Level}}</td><td>{{.IP}}</td><td>{{if .Status}}{{.Status}}{{end}}</td><td>{{.Message}}</td></tr>{{else}}<tr><td colspan="7">Записи не найдены</td></tr>{{end}}</tbody></table>
</section>
<script>
const rate = document.querySelector("#write-rate");
const saved = document.querySelector("#saved-total");
const status = document.querySelector("#metrics-status");
async function updateMetrics() {
  try {
    const response = await fetch("/metrics.json", {cache: "no-store", signal: AbortSignal.timeout(3000)});
    if (!response.ok) throw new Error(response.status);
    const metrics = await response.json();
    rate.textContent = metrics.rate.toLocaleString("ru");
    saved.textContent = metrics.saved.toLocaleString("ru");
    status.hidden = true;
  } catch {
    status.hidden = false;
  } finally {
    setTimeout(updateMetrics, 1000);
  }
}
updateMetrics();
</script>
</body>
</html>
`
