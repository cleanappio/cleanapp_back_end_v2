"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireWildcard(require("react"));
var _reactNative = require("react-native");
var _RNMBXImageNativeComponent = _interopRequireDefault(require("../specs/RNMBXImageNativeComponent"));
var _NativeRNMBXImageModule = _interopRequireDefault(require("../specs/NativeRNMBXImageModule"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
function _getRequireWildcardCache(e) { if ("function" != typeof WeakMap) return null; var r = new WeakMap(), t = new WeakMap(); return (_getRequireWildcardCache = function (e) { return e ? t : r; })(e); }
function _interopRequireWildcard(e, r) { if (!r && e && e.__esModule) return e; if (null === e || "object" != typeof e && "function" != typeof e) return { default: e }; var t = _getRequireWildcardCache(r); if (t && t.has(e)) return t.get(e); var n = { __proto__: null }, a = Object.defineProperty && Object.getOwnPropertyDescriptor; for (var u in e) if ("default" !== u && {}.hasOwnProperty.call(e, u)) { var i = a ? Object.getOwnPropertyDescriptor(e, u) : null; i && (i.get || i.set) ? Object.defineProperty(n, u, i) : n[u] = e[u]; } return n.default = e, t && t.set(e, n), n; }
const Image = /*#__PURE__*/(0, _react.memo)(/*#__PURE__*/(0, _react.forwardRef)(function Image({
  name,
  sdf,
  stretchX,
  stretchY,
  children
}, ref) {
  const nativeProps = {
    name,
    sdf,
    stretchX,
    stretchY,
    children
  };
  const imageRef = _react.default.useRef(null);
  const refresh = () => {
    const handle = (0, _reactNative.findNodeHandle)(imageRef.current);
    _NativeRNMBXImageModule.default.refresh(handle);
  };
  _react.default.useImperativeHandle(ref, () => {
    return {
      refresh
    };
  });

  // @ts-expect-error just codegen stuff
  return /*#__PURE__*/(0, _jsxRuntime.jsx)(_RNMBXImageNativeComponent.default, {
    ...nativeProps,
    ref: imageRef
  });
}));
Image.displayName = 'Image';
var _default = exports.default = Image;
//# sourceMappingURL=Image.js.map