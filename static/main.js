var term,
    socket

var terminalContainer = document.getElementById('terminal-container'),
    actionElements = {
      findText: document.getElementById('find-text'),
      findNext: document.getElementById('find-next'),
      findPrevious: document.getElementById('find-previous'),
      toggleOptions: document.getElementById('toggle-options'),
    },
    loginElements = {
      user: document.getElementById('userName'),
      password: document.getElementById('password'),
      login: document.getElementById('ssh-login'),
    },
    optionElements = {
      cursorBlink: document.getElementById('option-cursor-blink'),
      cursorStyle: document.getElementById('option-cursor-style'),
      scrollback: document.getElementById('option-scrollback'),
      tabstopwidth: document.getElementById('option-tabstopwidth'),
      bellStyle: document.getElementById('option-bell-style')
    },
    colsElement = document.getElementById('cols'),
    rowsElement = document.getElementById('rows');


var urlPrefix = getQueryStringByName("url_prefix")
var protocol = getQueryStringByName("protocol")
var hostname = getQueryStringByName("hostname")
var file = getQueryStringByName("file")
var port = getQueryStringByName("port")
var cmd = getQueryStringByName("cmd")
var is_debug = getQueryStringByName("debug")
var user = getQueryStringByName("user")
var password = getQueryStringByName("password")

//根据QueryString参数名称获取值
function getQueryStringByName(name) {
  var result = location.search.match(new RegExp("[\?\&]" + name + "=([^\&]+)", "i"));
  if (result == null || result.length < 1) {
      return "";
  }
  return result[1];
}

function startsWith(s, prefix) {
  return s.indexOf(prefix) == 0;
}

function changeClassList(ele, add, del) {
    var klsList = ele.classList;
    klsList.add(add);
    klsList.remove(del);
}

function toggleLogin() {
    var loginEl = document.getElementById("login");
    var optionsEl = document.getElementById("options");

    changeClassList(optionsEl, "hide", "active")
    
    var klsList = loginEl.classList;
    if (klsList.contains("hide")) {
      changeClassList(loginEl, "active", "hide")
    } else {
      changeClassList(loginEl, "hide", "active")
    }
}

function toggleLogin() {
    var loginEl = document.getElementById("login");
    var optionsEl = document.getElementById("options");

    changeClassList(optionsEl, "hide", "active")
    
    var klsList = loginEl.classList;
    if (klsList.contains("hide")) {
      changeClassList(loginEl, "active", "hide")
    } else {
      changeClassList(loginEl, "hide", "active")
    }
}


function toggleOptions() {
    var loginEl = document.getElementById("login");
    var optionsEl = document.getElementById("options");

    changeClassList(loginEl, "hide", "active")

    var klsList = optionsEl.classList;
    if (klsList.contains("hide")) {
      changeClassList(optionsEl, "active", "hide")
    } else {
      changeClassList(optionsEl, "hide", "active")
    }
}

actionElements.findNext.addEventListener('click', function() {
    term.findNext(actionElements.findText.value);
});
actionElements.findPrevious.addEventListener('click', function() {
    term.findPrevious(actionElements.findText.value);
});
actionElements.toggleOptions.addEventListener('click',  function() {
  toggleOptions();
});
loginElements.login.addEventListener('click', function() {
    user = loginElements.user.value;
    password = loginElements.password.value;

    toggleLogin();
    connect();
});

function setTerminalSize() {
  var cols = parseInt(colsElement.value, 10);
  var rows = parseInt(rowsElement.value, 10);
  var viewportElement = document.querySelector('.xterm-viewport');
  var scrollBarWidth = viewportElement.offsetWidth - viewportElement.clientWidth;
  var width = (cols * term.charMeasure.width + 20 /*room for scrollbar*/).toString() + 'px';
  var height = (rows * term.charMeasure.height).toString() + 'px';

  terminalContainer.style.width = width;
  terminalContainer.style.height = height;
  term.resize(cols, rows);
}

colsElement.addEventListener('change', setTerminalSize);
rowsElement.addEventListener('change', setTerminalSize);


optionElements.cursorBlink.addEventListener('change', function () {
  term.setOption('cursorBlink', optionElements.cursorBlink.checked);
});
optionElements.cursorStyle.addEventListener('change', function () {
  term.setOption('cursorStyle', optionElements.cursorStyle.value);
});
optionElements.bellStyle.addEventListener('change', function () {
  term.setOption('bellStyle', optionElements.bellStyle.value);
});
optionElements.scrollback.addEventListener('change', function () {
  term.setOption('scrollback', parseInt(optionElements.scrollback.value, 10));
});
optionElements.tabstopwidth.addEventListener('change', function () {
  term.setOption('tabStopWidth', parseInt(optionElements.tabstopwidth.value, 10));
});

function connect() {
    if(protocol == "ssh") {
      if (undefined == password || null == password || "" == password) {
        toggleLogin()
        return
      }
    }

    var target_url = "ws://" + document.location.host + urlPrefix + "/" + protocol + "?hostname=" + hostname + "&port=" + port + "&user=" + user + "&password=" + password + "&debug=" + is_debug
    if ("replay" == protocol) {
        target_url = "ws://" + document.location.host + urlPrefix + "/" + protocol + "?file=" + file + "&user=" + user + "&password=" + password
    } else if ("ssh_exec" == protocol) {
        target_url = "ws://" + document.location.host + urlPrefix + "/" + protocol + "?dump_file=" + file + "&hostname=" + hostname + "&port=" + port + "&user=" + user + "&password=" + password + "&cmd=" + cmd + "&debug=" + is_debug
    }

    createTerminal(target_url);
}

function createTerminal(targetUrl) {
  // Clean terminal
  while (terminalContainer.children.length) {
    terminalContainer.removeChild(terminalContainer.children[0]);
  }
  term = new Terminal({
    cursorBlink: optionElements.cursorBlink.checked,
    scrollback: parseInt(optionElements.scrollback.value, 10),
    tabStopWidth: parseInt(optionElements.tabstopwidth.value, 10)
  });
  term.on('resize', function (size) {
    //if (!pid) {
    //  return;
    //}
    //var cols = size.cols,
    //    rows = size.rows,
    //    url = '/terminals/' + pid + '/size?cols=' + cols + '&rows=' + rows;

    //fetch(url, {method: 'POST'});
  });

  term.open(terminalContainer);
  term.fit();

  // fit is called within a setTimeout, cols and rows need this.
  setTimeout(function () {
    colsElement.value = term.cols;
    rowsElement.value = term.rows;

    // Set terminal size again to set the specific dimensions on the demo
    setTerminalSize();

    socket = new WebSocket(targetUrl + '&columns=' + term.cols + '&rows=' + term.rows);
    socket.onopen = function() {
      term.attach(socket);
      term._initialized = true;
    };
    socket.onclose = function() {
      //term.destroy();
    };
    socket.onerror = function() {
      alert("连接出错！");
    };
  }, 0);
}

window.addEventListener('load', function () {
    if (undefined == protocol || null == protocol || "" == protocol) {
        protocol = "ssh"
        if (undefined == port || null == port || "" == port) {
            port = "22"
        }
    } else if ("telnet" == protocol) {
        if (undefined == port || null == port || "" == port) {
            port = "23"
        }
    } else if ("ssh" == protocol) {
        if (undefined == port || null == port || "" == port) {
            port = "22"
        }
    }

    if ("replay" == protocol) {
        if (undefined == file || null == file || "" == file) {
            alert("file is empty.")
            return
        }
    } else {
        if (undefined == hostname || null == hostname || "" == hostname) {
            alert("hostname is empty.")
            return
        }
    }

    if(undefined != urlPrefix && null != urlPrefix && "" != urlPrefix) {
      if (urlPrefix[urlPrefix.length-1] == "/") {
        urlPrefix = urlPrefix.substr(0, urlPrefix.length-1)
      }
    }

    if(undefined != urlPrefix && null != urlPrefix && "" != urlPrefix) {
      if (urlPrefix.indexOf("/") != 0) {
        urlPrefix = "/" + urlPrefix
      }
    }

    connect()
}, false);