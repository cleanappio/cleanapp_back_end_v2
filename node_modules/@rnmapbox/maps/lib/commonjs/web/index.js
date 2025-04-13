"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
Object.defineProperty(exports, "Camera", {
  enumerable: true,
  get: function () {
    return _Camera.default;
  }
});
Object.defineProperty(exports, "Logger", {
  enumerable: true,
  get: function () {
    return _Logger.default;
  }
});
Object.defineProperty(exports, "MapView", {
  enumerable: true,
  get: function () {
    return _MapView.default;
  }
});
Object.defineProperty(exports, "MarkerView", {
  enumerable: true,
  get: function () {
    return _MarkerView.default;
  }
});
exports.default = void 0;
require("mapbox-gl/dist/mapbox-gl.css");
var _MapboxModule = _interopRequireDefault(require("./MapboxModule"));
var _Camera = _interopRequireDefault(require("./components/Camera"));
var _MapView = _interopRequireDefault(require("./components/MapView"));
var _MarkerView = _interopRequireDefault(require("./components/MarkerView"));
var _Logger = _interopRequireDefault(require("./utils/Logger"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const ExportedComponents = {
  Camera: _Camera.default,
  MapView: _MapView.default,
  Logger: _Logger.default,
  MarkerView: _MarkerView.default
};
const Mapbox = {
  ..._MapboxModule.default,
  ...ExportedComponents
};
var _default = exports.default = Mapbox;
//# sourceMappingURL=index.js.map