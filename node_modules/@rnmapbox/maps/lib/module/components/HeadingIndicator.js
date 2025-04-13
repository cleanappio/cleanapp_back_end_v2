"use strict";

import React from 'react';
import headingIcon from '../assets/heading.png';
import { SymbolLayer } from './SymbolLayer';
import Images from './Images';
import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
const style = {
  iconImage: 'userLocationHeading',
  iconAllowOverlap: true,
  iconPitchAlignment: 'map',
  iconRotationAlignment: 'map'
};
const HeadingIndicator = ({
  heading
}) => {
  return /*#__PURE__*/_jsxs(React.Fragment, {
    children: [/*#__PURE__*/_jsx(Images, {
      images: {
        userLocationHeading: headingIcon
      }
    }, "mapboxUserLocationHeadingImages"), /*#__PURE__*/_jsx(SymbolLayer, {
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
export default HeadingIndicator;
//# sourceMappingURL=HeadingIndicator.js.map