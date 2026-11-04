# Third-party source notices

## React Bits — CSS StarBorder

`web/src/components/ui/StarButton.tsx` and `StarButton.css` adapt the small,
CSS-only StarBorder variant from React Bits, copyright (c) 2025 David Haz.

Pinned revision: `c83b5e7e026f003af33ff9ebc3d93afb5c86ad22`.

- [Original component](https://github.com/DavidHDev/react-bits/blob/c83b5e7e026f003af33ff9ebc3d93afb5c86ad22/src/ts-default/Animations/StarBorder/StarBorder.tsx)
- [Original CSS](https://github.com/DavidHDev/react-bits/blob/c83b5e7e026f003af33ff9ebc3d93afb5c86ad22/src/ts-default/Animations/StarBorder/StarBorder.css)
- [Included license](third_party/react-bits/LICENSE.md): MIT + Commons Clause.

Local adaptations use a native button, phrasing-content spans, application
colors, disabled/focus states and reduced-motion handling. This variant was
chosen instead of the newer canvas implementation to avoid a continuous
JavaScript drawing loop or additional animation dependencies.

The component is incorporated into this application, not distributed as a
standalone component library. Its license has additional restrictions beyond
plain MIT; retain the included notice when distributing the application.
