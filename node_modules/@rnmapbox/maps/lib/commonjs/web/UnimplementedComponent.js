"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const UnimplementedComponent = name => class SymbolLater extends _react.default.Component {
  render() {
    return /*#__PURE__*/(0, _jsxRuntime.jsxs)("div", {
      children: ["TODO implement ", name]
    });
  }
};
var _default = exports.default = UnimplementedComponent;
//# sourceMappingURL=UnimplementedComponent.js.map