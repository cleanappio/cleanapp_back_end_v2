"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.default = void 0;
var _react = _interopRequireDefault(require("react"));
var _heading = _interopRequireDefault(require("../assets/heading.png"));
var _SymbolLayer = require("./SymbolLayer");
var _Images = _interopRequireDefault(require("./Images"));
var _jsxRuntime = require("react/jsx-runtime");
function _interopRequireDefault(e) { return e && e.__esModule ? e : { default: e }; }
const style = {
  iconImage: 'userLocationHeading',
  iconAllowOverlap: true,
  iconPitchAlignment: 'map',
  iconRotationAlignment: 'map'
};
const HeadingIndicator = ({
  heading
}) => {
  return /*#__PURE__*/(0, _jsxRuntime.jsxs)(_react.default.Fragment, {
    children: [/*#__PURE__*/(0, _jsxRuntime.jsx)(_Images.default, {
      images: {
        userLocationHeading: _heading.default
      }
    }, "mapboxUserLocationHeadingImages"), /*#__PURE__*/(0, _jsxRuntime.jsx)(_SymbolLayer.SymbolLayer, {
      id: "mapboxUserLocationHeadingIndicator",
      sourceID: "mapboxUserLocation",
      belowLayerID: "mapboxUserLocationWhiteCircle",
      style: {
        iconRotate: heading,
        ...style
      }
    }, "mapboxUserLocationHeadingIndicator")]
  }, "mapboxUserLocationHeadingIndicatorWrapper");
};
var _default = exports.default = HeadingIndicator;
//# sourceMappingURL=HeadingIndicator.js.map