// Live reload: go-grip streams a "reload" event when something this page
// shows has changed (the Markdown file, an image it references, or for a
// folder page, the folder's entries).
(function () {
  if (!window.EventSource) return;

  var pagePath = location.pathname;
  try {
    pagePath = decodeURIComponent(pagePath);
  } catch (e) {}

  var source = new EventSource("/__grip/events?path=" + encodeURIComponent(pagePath));
  var lostConnection = false;

  source.addEventListener("reload", function () {
    source.close();
    location.reload();
  });

  source.onerror = function () {
    lostConnection = true;
  };

  // The connection came back after an error: the server probably restarted,
  // so the page may be stale.
  source.onopen = function () {
    if (lostConnection) {
      source.close();
      location.reload();
    }
  };
})();
