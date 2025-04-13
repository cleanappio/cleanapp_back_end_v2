"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _mapboxGl = _interopRequireDefault(require("mapbox-gl"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const MapboxModule = {
  LineJoin: {},
  StyleURL: {
    Street: 'mapbox://styles/mapbox/streets-v11',
    Satellite: 'mapbox://styles/mapbox/satellite-v9'
  },
  setAccessToken: token => {
    _mapboxGl.default.accessToken = token;
  }
};
var _default = exports.default = MapboxModule;
//# sourceMappingURL=MapboxModule.js.map